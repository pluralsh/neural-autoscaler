package resize

import (
	"math"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

const defaultHeadroomFactor = 1.2

const (
	bytesPerKi = 1024
	bytesPerMi = 1024 * bytesPerKi
	bytesPerGi = 1024 * bytesPerMi
)

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
			normalized := normalizeMemoryQuantity(clamped)
			normalized = autoscalingv1alpha1.ClampQuantity(normalized, bounds)
			out.Memory = &normalized
		}
	}

	return out
}

// normalizeMemoryQuantity rounds memory up to whole Ki, Mi, or Gi so pod specs
// use human-readable units instead of raw byte strings.
func normalizeMemoryQuantity(q resource.Quantity) resource.Quantity {
	bytes := q.Value()
	if bytes <= 0 {
		return q
	}

	var unit string
	var whole int64
	switch {
	case bytes >= bytesPerGi:
		unit = "Gi"
		whole = (bytes + bytesPerGi - 1) / bytesPerGi
	case bytes >= bytesPerMi:
		unit = "Mi"
		whole = (bytes + bytesPerMi - 1) / bytesPerMi
	case bytes >= bytesPerKi:
		unit = "Ki"
		whole = (bytes + bytesPerKi - 1) / bytesPerKi
	default:
		return q
	}

	normalized := resource.MustParse(strconv.FormatInt(whole, 10) + unit)
	if normalized.Cmp(q) < 0 {
		return q
	}
	return normalized
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
