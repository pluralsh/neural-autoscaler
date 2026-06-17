package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ClampQuantity applies min/max bounds from a resource entry.
func ClampQuantity(q resource.Quantity, bounds ResourceBoundsSpec) resource.Quantity {
	out := q.DeepCopy()
	minQ, maxQ, err := ResourceBounds(bounds)
	if err != nil {
		return out
	}
	if minQ != nil && out.Cmp(*minQ) < 0 {
		out = minQ.DeepCopy()
	}
	if maxQ != nil && out.Cmp(*maxQ) > 0 {
		out = maxQ.DeepCopy()
	}
	return out
}

// ResourceNameForMetric maps a ResourceMetric to its corev1.ResourceName.
func ResourceNameForMetric(metric ResourceMetric) corev1.ResourceName {
	switch metric {
	case ResourceMetricCPU:
		return corev1.ResourceCPU
	case ResourceMetricMemory:
		return corev1.ResourceMemory
	default:
		return corev1.ResourceName(metric)
	}
}
