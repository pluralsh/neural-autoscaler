package resize

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/log"
)

// Reconciler applies in-place pod resizes for a NeuralAutoscaler.
type Reconciler struct {
	Client client.Client
}

// Apply resizes pods resolved from metrics targetRef when per-resource forecast data is available.
// recentHistory supplies buffered reconcile-interval samples used to floor forecast peaks with
// the recent observed maximum (helps bursty CPU where Chronos median forecasts lag spikes).
func (r *Reconciler) Apply(ctx context.Context, na *autoscalingv1alpha1.NeuralAutoscaler, forecasts map[autoscalingv1alpha1.ResourceMetric]forecast.Response, recentHistory map[autoscalingv1alpha1.ResourceMetric][]float64, podNames []string, namespace string) error {
	if na.Spec.Resize == nil {
		return nil
	}
	if err := autoscalingv1alpha1.ValidateResize(na.Spec.Resize); err != nil {
		return err
	}

	naKey := client.ObjectKeyFromObject(na)
	spec := na.Spec.Resize
	if len(podNames) == 0 {
		log.Info("no pods to resize", "neuralAutoscaler", naKey, "namespace", namespace)
		return nil
	}

	forecastPeaks := make(map[autoscalingv1alpha1.ResourceMetric]float64, len(forecasts))
	for resource, resp := range forecasts {
		quantiles := make([]QuantileSeries, 0, len(resp.Quantiles))
		for _, q := range resp.Quantiles {
			quantiles = append(quantiles, QuantileSeries{Level: q.Level, Values: q.Values})
		}
		peak := ForecastPeaks(resp.Point, quantiles)
		if history, ok := recentHistory[resource]; ok {
			observed := RecentPeak(history, DefaultRecentPeakWindow)
			if raised := EffectivePeak(peak, observed); raised > peak {
				log.Debug("raising forecast peak with recent observed maximum",
					"neuralAutoscaler", naKey,
					"resource", resource,
					"forecastPeak", peak,
					"observedPeak", observed,
					"effectivePeak", raised,
					"recentWindow", DefaultRecentPeakWindow,
				)
				peak = raised
			}
		}
		forecastPeaks[resource] = peak
	}

	eligiblePods, err := r.listEligiblePods(ctx, namespace, podNames)
	if err != nil {
		return err
	}
	if len(eligiblePods) == 0 {
		log.Info("no eligible pods for resize", "neuralAutoscaler", naKey, "namespace", namespace, "requested", len(podNames))
		return nil
	}

	podCount := len(eligiblePods)
	for _, pod := range eligiblePods {
		if err := r.resizePod(ctx, naKey, pod, spec, forecastPeaks, podCount); err != nil {
			log.Error(err, "pod resize failed", "neuralAutoscaler", naKey, "pod", client.ObjectKeyFromObject(&pod))
		}
	}
	return nil
}

func (r *Reconciler) listEligiblePods(ctx context.Context, namespace string, podNames []string) ([]corev1.Pod, error) {
	eligible := make([]corev1.Pod, 0, len(podNames))
	for _, name := range podNames {
		pod := &corev1.Pod{}
		key := client.ObjectKey{Namespace: namespace, Name: name}
		if err := r.Client.Get(ctx, key, pod); err != nil {
			if apierrors.IsNotFound(err) {
				log.Warning("skipping resize: pod not found", "pod", key)
				continue
			}
			return nil, fmt.Errorf("get pod %q: %w", key, err)
		}
		if pod.DeletionTimestamp != nil {
			log.Debug("skipping terminating pod", "pod", name)
			continue
		}
		if resizeInProgress(pod) {
			log.Info("skipping pod with resize in progress", "pod", name)
			continue
		}
		eligible = append(eligible, *pod)
	}
	return eligible, nil
}

func (r *Reconciler) resizePod(ctx context.Context, naKey client.ObjectKey, pod corev1.Pod, spec *autoscalingv1alpha1.ResizeSpec, forecastPeaks map[autoscalingv1alpha1.ResourceMetric]float64, podCount int) error {
	if pod.DeletionTimestamp != nil {
		return nil
	}
	if resizeInProgress(&pod) {
		log.Info("skipping pod with resize in progress", "neuralAutoscaler", naKey, "pod", pod.Name)
		return nil
	}

	containerIndexes := targetContainerIndexes(pod, spec)
	if len(containerIndexes) == 0 {
		if spec.ContainerName != nil {
			log.Warning(
				"skipping pod resize: target container not found",
				"neuralAutoscaler", naKey,
				"pod", pod.Name,
				"targetContainer", *spec.ContainerName,
			)
			return nil
		}
		log.Warning("skipping pod resize: pod has no containers", "neuralAutoscaler", naKey, "pod", pod.Name)
		return nil
	}

	plans := make([]containerResizePlan, 0, len(containerIndexes))
	for _, idx := range containerIndexes {
		current := pod.Spec.Containers[idx].Resources.Requests
		targets := ComputeTargets(TargetInput{
			ForecastPeaks:   forecastPeaks,
			PodCount:        podCount,
			CurrentRequests: current,
			Resources:       spec.Resources,
		})
		targets, changed := ApplyMinChangeThreshold(current, targets, spec.MinChangePercent, spec.Resources)
		if !changed || !targetsChanged(current, targets) {
			continue
		}
		plans = append(plans, containerResizePlan{
			Index:   idx,
			Name:    pod.Spec.Containers[idx].Name,
			Current: current,
			Targets: targets,
		})
	}
	if len(plans) == 0 {
		log.Warning(
			"skipping pod resize: change below minChangePercent threshold",
			"neuralAutoscaler", naKey,
			"pod", pod.Name,
			"minChangePercent", formatMinChangePercent(spec),
		)
		return nil
	}

	resizePod := buildResizePod(pod, plans, spec.Resources)
	log.Info("resizing pod in place",
		"neuralAutoscaler", naKey,
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"containers", containerNames(plans),
		"cpu", formatContainerChanges(plans, corev1.ResourceCPU),
		"memory", formatContainerChanges(plans, corev1.ResourceMemory),
	)

	if err := r.Client.SubResource("resize").Update(ctx, resizePod); err != nil {
		return fmt.Errorf("update resize subresource: %w", err)
	}
	return nil
}

type containerResizePlan struct {
	Index   int
	Name    string
	Current corev1.ResourceList
	Targets TargetResult
}

func targetContainerIndexes(pod corev1.Pod, spec *autoscalingv1alpha1.ResizeSpec) []int {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}
	if spec != nil && spec.ContainerName != nil {
		name := *spec.ContainerName
		if name == "*" {
			indexes := make([]int, len(pod.Spec.Containers))
			for i := range pod.Spec.Containers {
				indexes[i] = i
			}
			return indexes
		}
		for i, c := range pod.Spec.Containers {
			if c.Name == name {
				return []int{i}
			}
		}
		return nil
	}
	return []int{0}
}

func buildResizePod(pod corev1.Pod, plans []containerResizePlan, resources map[string]autoscalingv1alpha1.ResourceBoundsSpec) *corev1.Pod {
	controlled := autoscalingv1alpha1.ControlledResourceSet(resources)
	out := pod.DeepCopy()
	for _, plan := range plans {
		if plan.Index < 0 || plan.Index >= len(out.Spec.Containers) {
			continue
		}
		reqs := out.Spec.Containers[plan.Index].Resources.Requests
		if reqs == nil {
			reqs = corev1.ResourceList{}
		}
		limits := out.Spec.Containers[plan.Index].Resources.Limits
		if limits == nil {
			limits = corev1.ResourceList{}
		}

		if controlled[corev1.ResourceCPU] && plan.Targets.CPU != nil {
			reqs[corev1.ResourceCPU] = plan.Targets.CPU.DeepCopy()
			if lim, ok := limits[corev1.ResourceCPU]; ok && lim.Cmp(*plan.Targets.CPU) < 0 {
				limits[corev1.ResourceCPU] = plan.Targets.CPU.DeepCopy()
			}
		}
		if controlled[corev1.ResourceMemory] && plan.Targets.Memory != nil {
			reqs[corev1.ResourceMemory] = plan.Targets.Memory.DeepCopy()
			if lim, ok := limits[corev1.ResourceMemory]; ok && lim.Cmp(*plan.Targets.Memory) < 0 {
				limits[corev1.ResourceMemory] = plan.Targets.Memory.DeepCopy()
			}
		}
		out.Spec.Containers[plan.Index].Resources.Requests = reqs
		out.Spec.Containers[plan.Index].Resources.Limits = limits
	}
	return out
}

func targetsChanged(current corev1.ResourceList, targets TargetResult) bool {
	if targets.CPU != nil {
		if cur, ok := current[corev1.ResourceCPU]; !ok || cur.Cmp(*targets.CPU) != 0 {
			return true
		}
	}
	if targets.Memory != nil {
		if cur, ok := current[corev1.ResourceMemory]; !ok || cur.Cmp(*targets.Memory) != 0 {
			return true
		}
	}
	return false
}

func resizeInProgress(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodResizeInProgress && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func formatMinChangePercent(spec *autoscalingv1alpha1.ResizeSpec) any {
	if spec.MinChangePercent != nil {
		return *spec.MinChangePercent
	}
	return autoscalingv1alpha1.DefaultMinChangePercent
}

func formatChange(current corev1.ResourceList, name corev1.ResourceName, desired *resource.Quantity) string {
	old := "unset"
	if q, ok := current[name]; ok {
		old = q.String()
	}
	newVal := "unchanged"
	if desired != nil {
		newVal = desired.String()
	}
	return old + " -> " + newVal
}

func containerNames(plans []containerResizePlan) []string {
	names := make([]string, 0, len(plans))
	for _, plan := range plans {
		names = append(names, plan.Name)
	}
	return names
}

func formatContainerChanges(plans []containerResizePlan, name corev1.ResourceName) []string {
	changes := make([]string, 0, len(plans))
	for _, plan := range plans {
		changes = append(changes, plan.Name+": "+formatChange(plan.Current, name, targetForResource(plan.Targets, name)))
	}
	return changes
}

func targetForResource(targets TargetResult, name corev1.ResourceName) *resource.Quantity {
	switch name {
	case corev1.ResourceCPU:
		return targets.CPU
	case corev1.ResourceMemory:
		return targets.Memory
	default:
		return nil
	}
}
