package resize

import (
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

const defaultHeadroomFactor = 1.2

// TargetInput supplies forecast and workload context for per-pod target calculation.
type TargetInput struct {
	ForecastPeaks   map[autoscalingv1alpha1.ResourceMetric]float64
	PodCount        int
	CurrentRequests corev1.ResourceList
	Resources       map[string]autoscalingv1alpha1.ResourceBoundsSpec
}

// TargetResult holds desired container resource requests for controlled resources.
type TargetResult struct {
	CPU    *resource.Quantity
	Memory *resource.Quantity
}

// ComputeTargets maps a cluster-wide forecast peak to per-pod container requests:
// (peak × defaultHeadroomFactor) / podCount, then clamped to spec.resize.resources
// min/max. Current requests are retained when no forecast peak exists for a resource.
func ComputeTargets(in TargetInput) TargetResult {
	out := TargetResult{}
	if in.PodCount <= 0 {
		in.PodCount = 1
	}

	for key, bounds := range in.Resources {
		switch autoscalingv1alpha1.ResourceMetric(key) {
		case autoscalingv1alpha1.ResourceMetricCPU:
			desired := currentQuantity(in.CurrentRequests, corev1.ResourceCPU)
			if peak, ok := in.ForecastPeaks[autoscalingv1alpha1.ResourceMetricCPU]; ok && peak > 0 {
				perPodMilli := (peak * defaultHeadroomFactor) / float64(in.PodCount)
				desired = resource.NewMilliQuantity(int64(math.Ceil(perPodMilli)), resource.DecimalSI)
			}
			clamped := autoscalingv1alpha1.ClampQuantity(*desired, bounds)
			out.CPU = &clamped
		case autoscalingv1alpha1.ResourceMetricMemory:
			desired := currentQuantity(in.CurrentRequests, corev1.ResourceMemory)
			if peak, ok := in.ForecastPeaks[autoscalingv1alpha1.ResourceMetricMemory]; ok && peak > 0 {
				perPodBytes := (peak * defaultHeadroomFactor) / float64(in.PodCount)
				desired = resource.NewQuantity(int64(math.Ceil(perPodBytes)), resource.BinarySI)
			}
			clamped := autoscalingv1alpha1.ClampQuantity(*desired, bounds)
			out.Memory = &clamped
		}
	}

	return out
}

func currentQuantity(list corev1.ResourceList, name corev1.ResourceName) *resource.Quantity {
	if q, ok := list[name]; ok {
		copied := q.DeepCopy()
		return &copied
	}
	return resource.NewQuantity(0, resource.DecimalSI)
}

// ForecastPeaks returns the maximum value across forecast points and optional quantile series.
func ForecastPeaks(points []float64, quantiles []QuantileSeries) (peak float64) {
	for _, v := range points {
		if v > peak {
			peak = v
		}
	}
	for _, q := range quantiles {
		for _, v := range q.Values {
			if v > peak {
				peak = v
			}
		}
	}
	return peak
}

// QuantileSeries mirrors forecast.QuantileSeries for peak calculation without importing forecast.
type QuantileSeries struct {
	Level  float64
	Values []float64
}
