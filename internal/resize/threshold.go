package resize

import (
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

// ApplyMinChangeThreshold retains current requests for resources whose desired
// change is below the configured percent threshold. Returns adjusted targets and
// whether any controlled resource still differs from current after thresholding.
func ApplyMinChangeThreshold(
	current corev1.ResourceList,
	targets TargetResult,
	globalMin *int32,
	resources map[string]autoscalingv1alpha1.ResourceBoundsSpec,
) (TargetResult, bool) {
	out := targets
	anyChanged := false

	if targets.CPU != nil {
		if bounds, ok := resources[string(autoscalingv1alpha1.ResourceMetricCPU)]; ok {
			cur := currentQuantity(current, corev1.ResourceCPU)
			threshold := autoscalingv1alpha1.EffectiveMinChangePercent(globalMin, bounds.MinChangePercent)
			if exceedsMinChangeThreshold(*cur, *targets.CPU, threshold) {
				anyChanged = true
			} else {
				kept := cur.DeepCopy()
				out.CPU = &kept
			}
		}
	}

	if targets.Memory != nil {
		if bounds, ok := resources[string(autoscalingv1alpha1.ResourceMetricMemory)]; ok {
			cur := currentQuantity(current, corev1.ResourceMemory)
			threshold := autoscalingv1alpha1.EffectiveMinChangePercent(globalMin, bounds.MinChangePercent)
			if exceedsMinChangeThreshold(*cur, *targets.Memory, threshold) {
				anyChanged = true
			} else {
				kept := cur.DeepCopy()
				out.Memory = &kept
			}
		}
	}

	return out, anyChanged
}

func exceedsMinChangeThreshold(old, new resource.Quantity, thresholdPercent int32) bool {
	if old.IsZero() {
		return new.Sign() > 0
	}
	if new.Cmp(old) == 0 {
		return false
	}
	return quantityPercentChange(old, new) >= float64(thresholdPercent)
}

func quantityPercentChange(old, new resource.Quantity) float64 {
	oldMag := quantityMagnitude(old)
	if oldMag == 0 {
		return 0
	}
	diff := math.Abs(quantityMagnitude(new) - oldMag)
	return diff / oldMag * 100
}

func quantityMagnitude(q resource.Quantity) float64 {
	if q.IsZero() {
		return 0
	}
	if milli := q.MilliValue(); milli != 0 {
		return float64(milli)
	}
	return float64(q.Value())
}
