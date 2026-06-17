package resize

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

// Reconciler applies in-place pod resizes for a NeuralAutoscaler.
type Reconciler struct {
	Client client.Client
}

// Apply resizes pods resolved from metrics targetRef when per-resource forecast data is available.
// recentHistory supplies buffered metrics-server samples used to floor forecast peaks with
// the recent observed maximum (helps bursty CPU where Chronos median forecasts lag spikes).
func (r *Reconciler) Apply(ctx context.Context, na *autoscalingv1alpha1.NeuralAutoscaler, forecasts map[autoscalingv1alpha1.ResourceMetric]forecast.Response, recentHistory map[autoscalingv1alpha1.ResourceMetric][]float64, podNames []string, namespace string) error {
	if na.Spec.Resize == nil {
		return nil
	}
	if err := autoscalingv1alpha1.ValidateResize(na.Spec.Resize); err != nil {
		return err
	}

	logger := log.FromContext(ctx)
	spec := na.Spec.Resize
	if len(podNames) == 0 {
		logger.Info("no pods to resize", "namespace", namespace)
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
				logger.V(1).Info("raising forecast peak with recent observed maximum",
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
		logger.Info("no eligible pods for resize", "namespace", namespace, "requested", len(podNames))
		return nil
	}

	podCount := len(eligiblePods)
	for _, pod := range eligiblePods {
		if err := r.resizePod(ctx, pod, spec, forecastPeaks, podCount); err != nil {
			logger.Error(err, "pod resize failed", "pod", client.ObjectKeyFromObject(&pod))
		}
	}
	return nil
}

func (r *Reconciler) listEligiblePods(ctx context.Context, namespace string, podNames []string) ([]corev1.Pod, error) {
	logger := log.FromContext(ctx)
	eligible := make([]corev1.Pod, 0, len(podNames))
	for _, name := range podNames {
		pod := &corev1.Pod{}
		key := client.ObjectKey{Namespace: namespace, Name: name}
		if err := r.Client.Get(ctx, key, pod); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("skipping resize: pod not found", "pod", key)
				continue
			}
			return nil, fmt.Errorf("get pod %q: %w", key, err)
		}
		if pod.DeletionTimestamp != nil {
			logger.V(1).Info("skipping terminating pod", "pod", name)
			continue
		}
		if resizeInProgress(pod) {
			logger.Info("skipping pod with resize in progress", "pod", name)
			continue
		}
		eligible = append(eligible, *pod)
	}
	return eligible, nil
}

func (r *Reconciler) resizePod(ctx context.Context, pod corev1.Pod, spec *autoscalingv1alpha1.ResizeSpec, forecastPeaks map[autoscalingv1alpha1.ResourceMetric]float64, podCount int) error {
	if pod.DeletionTimestamp != nil {
		return nil
	}
	if resizeInProgress(&pod) {
		log.FromContext(ctx).Info("skipping pod with resize in progress", "pod", pod.Name)
		return nil
	}

	current := primaryContainerRequests(pod)
	targets := ComputeTargets(TargetInput{
		ForecastPeaks:   forecastPeaks,
		PodCount:        podCount,
		CurrentRequests: current,
		Resources:       spec.Resources,
	})
	targets, changed := ApplyMinChangeThreshold(current, targets, spec.MinChangePercent, spec.Resources)
	if !changed {
		log.FromContext(ctx).V(1).Info(
			"skipping pod resize: change below minChangePercent threshold",
			"pod", pod.Name,
			"minChangePercent", formatMinChangePercent(spec),
		)
		return nil
	}
	if !targetsChanged(current, targets) {
		return nil
	}

	resizePod := buildResizePod(pod, targets, spec.Resources)
	logger := log.FromContext(ctx)
	logger.Info("resizing pod in place",
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"cpu", formatChange(current, corev1.ResourceCPU, targets.CPU),
		"memory", formatChange(current, corev1.ResourceMemory, targets.Memory),
	)

	if err := r.Client.SubResource("resize").Update(ctx, resizePod); err != nil {
		return fmt.Errorf("update resize subresource: %w", err)
	}
	return nil
}

func primaryContainerRequests(pod corev1.Pod) corev1.ResourceList {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}
	return pod.Spec.Containers[0].Resources.Requests
}

func buildResizePod(pod corev1.Pod, targets TargetResult, resources map[string]autoscalingv1alpha1.ResourceBoundsSpec) *corev1.Pod {
	controlled := autoscalingv1alpha1.ControlledResourceSet(resources)
	out := pod.DeepCopy()
	for i := range out.Spec.Containers {
		reqs := out.Spec.Containers[i].Resources.Requests
		if reqs == nil {
			reqs = corev1.ResourceList{}
		}
		limits := out.Spec.Containers[i].Resources.Limits
		if limits == nil {
			limits = corev1.ResourceList{}
		}

		if controlled[corev1.ResourceCPU] && targets.CPU != nil {
			reqs[corev1.ResourceCPU] = targets.CPU.DeepCopy()
			if lim, ok := limits[corev1.ResourceCPU]; ok && lim.Cmp(*targets.CPU) < 0 {
				limits[corev1.ResourceCPU] = targets.CPU.DeepCopy()
			}
		}
		if controlled[corev1.ResourceMemory] && targets.Memory != nil {
			reqs[corev1.ResourceMemory] = targets.Memory.DeepCopy()
			if lim, ok := limits[corev1.ResourceMemory]; ok && lim.Cmp(*targets.Memory) < 0 {
				limits[corev1.ResourceMemory] = targets.Memory.DeepCopy()
			}
		}
		out.Spec.Containers[i].Resources.Requests = reqs
		out.Spec.Containers[i].Resources.Limits = limits
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
