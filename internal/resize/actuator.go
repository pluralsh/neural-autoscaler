package resize

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

// Reconciler applies in-place pod resizes for a NeuralAutoscaler.
type Reconciler struct {
	Client client.Client
}

// Apply resizes pods matching spec.resize when per-resource forecast data is available.
func (r *Reconciler) Apply(ctx context.Context, na *autoscalingv1alpha1.NeuralAutoscaler, forecasts map[autoscalingv1alpha1.ResourceMetric]forecast.Response) error {
	if na.Spec.Resize == nil {
		return nil
	}
	if err := autoscalingv1alpha1.ValidateResize(na.Spec.Resize); err != nil {
		return err
	}

	logger := log.FromContext(ctx)
	spec := na.Spec.Resize
	namespace := na.Namespace
	if spec.Namespace != "" {
		namespace = spec.Namespace
	}

	selector, err := metav1.LabelSelectorAsSelector(&spec.TargetSelector)
	if err != nil {
		return fmt.Errorf("resize target selector: %w", err)
	}

	podList := &corev1.PodList{}
	if err := r.Client.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return fmt.Errorf("list pods for resize: %w", err)
	}
	if len(podList.Items) == 0 {
		logger.Info("no pods match resize target selector", "namespace", namespace, "selector", selector.String())
		return nil
	}

	forecastPeaks := make(map[autoscalingv1alpha1.ResourceMetric]float64, len(forecasts))
	for resource, resp := range forecasts {
		quantiles := make([]QuantileSeries, 0, len(resp.Quantiles))
		for _, q := range resp.Quantiles {
			quantiles = append(quantiles, QuantileSeries{Level: q.Level, Values: q.Values})
		}
		forecastPeaks[resource] = ForecastPeaks(resp.Point, quantiles)
	}

	podCount := len(podList.Items)
	for _, pod := range podList.Items {
		if err := r.resizePod(ctx, pod, spec.Resources, forecastPeaks, podCount); err != nil {
			logger.Error(err, "pod resize failed", "pod", client.ObjectKeyFromObject(&pod))
		}
	}
	return nil
}

func (r *Reconciler) resizePod(ctx context.Context, pod corev1.Pod, resources map[string]autoscalingv1alpha1.ResourceBoundsSpec, forecastPeaks map[autoscalingv1alpha1.ResourceMetric]float64, podCount int) error {
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
		Resources:       resources,
	})
	if !targetsChanged(current, targets) {
		return nil
	}

	resizePod := buildResizePod(pod, targets, resources)
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
