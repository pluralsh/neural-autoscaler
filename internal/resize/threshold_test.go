package resize

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

func int32Ptr(v int32) *int32 { return &v }

func TestQuantityPercentChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		old  string
		new  string
		want float64
	}{
		{
			name: "cpu one millicore change",
			old:  "101m",
			new:  "100m",
			want: 0.9900990099009901,
		},
		{
			name: "cpu fifty percent change",
			old:  "100m",
			new:  "150m",
			want: 50,
		},
		{
			name: "memory ten percent change",
			old:  "1Gi",
			new:  "1152Mi",
			want: 12.5,
		},
		{
			name: "unchanged",
			old:  "500m",
			new:  "500m",
			want: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := quantityPercentChange(resource.MustParse(tt.old), resource.MustParse(tt.new))
			if got != tt.want {
				t.Fatalf("quantityPercentChange(%s, %s) = %v, want %v", tt.old, tt.new, got, tt.want)
			}
		})
	}
}

func TestExceedsMinChangeThreshold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		old       string
		new       string
		threshold int32
		want      bool
	}{
		{
			name:      "tiny cpu change below default threshold",
			old:       "101m",
			new:       "100m",
			threshold: 5,
			want:      false,
		},
		{
			name:      "large cpu change above threshold",
			old:       "100m",
			new:       "150m",
			threshold: 5,
			want:      true,
		},
		{
			name:      "zero old with positive new counts as change",
			old:       "0",
			new:       "100m",
			threshold: 100,
			want:      true,
		},
		{
			name:      "unchanged values",
			old:       "500m",
			new:       "500m",
			threshold: 0,
			want:      false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := exceedsMinChangeThreshold(
				resource.MustParse(tt.old),
				resource.MustParse(tt.new),
				tt.threshold,
			)
			if got != tt.want {
				t.Fatalf("exceedsMinChangeThreshold() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyMinChangeThreshold(t *testing.T) {
	t.Parallel()

	resources := resizeResourcesFixture()
	current := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("101m"),
		corev1.ResourceMemory: resource.MustParse("1Gi"),
	}
	targets := TargetResult{
		CPU:    quantityPtr(resource.MustParse("100m")),
		Memory: quantityPtr(resource.MustParse("1Gi")),
	}

	adjusted, changed := ApplyMinChangeThreshold(current, targets, int32Ptr(5), resources)
	if changed {
		t.Fatal("expected no resize when cpu change is below threshold")
	}
	if adjusted.CPU == nil || adjusted.CPU.Cmp(resource.MustParse("101m")) != 0 {
		t.Fatalf("cpu = %v, want retained 101m", adjusted.CPU)
	}

	targets.CPU = quantityPtr(resource.MustParse("200m"))
	adjusted, changed = ApplyMinChangeThreshold(current, targets, int32Ptr(5), resources)
	if !changed {
		t.Fatal("expected resize when cpu change exceeds threshold")
	}
	if adjusted.CPU == nil || adjusted.CPU.Cmp(resource.MustParse("200m")) != 0 {
		t.Fatalf("cpu = %v, want 200m", adjusted.CPU)
	}
}

func TestApplyMinChangeThresholdPerResourceOverride(t *testing.T) {
	t.Parallel()

	resources := map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricCPU): {
			MinChangePercent: int32Ptr(1),
		},
	}
	current := corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("101m"),
	}
	targets := TargetResult{
		CPU: quantityPtr(resource.MustParse("99m")),
	}

	_, changed := ApplyMinChangeThreshold(current, targets, int32Ptr(10), resources)
	if !changed {
		t.Fatal("expected per-resource override to allow sub-global threshold change")
	}
}

func quantityPtr(q resource.Quantity) *resource.Quantity {
	copied := q.DeepCopy()
	return &copied
}
