package resize

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

func strPtr(s string) *string { return &s }

func TestComputeTargetsCPUFromForecast(t *testing.T) {
	t.Parallel()

	resources := resizeResourcesFixture()
	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricCPU: 1000,
		},
		PodCount: 2,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		},
		Resources: resources,
	}

	got := ComputeTargets(in)
	if got.CPU == nil {
		t.Fatal("expected CPU target")
	}
	// 1000m * 1.2 headroom / 2 pods = 600m
	want := resource.MustParse("600m")
	if got.CPU.Cmp(want) != 0 {
		t.Fatalf("CPU target = %s, want %s", got.CPU.String(), want.String())
	}
}

func TestComputeTargetsClampsMinMax(t *testing.T) {
	t.Parallel()

	resources := resizeResourcesFixture()
	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricCPU: 50,
		},
		PodCount: 1,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		},
		Resources: resources,
	}
	got := ComputeTargets(in)
	wantMin := resource.MustParse("250m")
	if got.CPU.Cmp(wantMin) != 0 {
		t.Fatalf("min clamp: got %s want %s", got.CPU.String(), wantMin.String())
	}

	in.ForecastPeaks[autoscalingv1alpha1.ResourceMetricCPU] = 20000
	got = ComputeTargets(in)
	wantMax := resource.MustParse("8")
	if got.CPU.Cmp(wantMax) != 0 {
		t.Fatalf("max clamp: got %s want %s", got.CPU.String(), wantMax.String())
	}
}

func TestComputeTargetsRetainsMemoryWithoutForecast(t *testing.T) {
	t.Parallel()

	resources := resizeResourcesFixture()
	currentMem := resource.MustParse("1Gi")
	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricCPU: 500,
		},
		PodCount: 1,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: currentMem,
		},
		Resources: resources,
	}

	got := ComputeTargets(in)
	if got.Memory == nil {
		t.Fatal("expected memory target")
	}
	if got.Memory.Cmp(currentMem) != 0 {
		t.Fatalf("memory = %s, want retained %s", got.Memory.String(), currentMem.String())
	}
}

func TestComputeTargetsMemoryFromForecast(t *testing.T) {
	t.Parallel()

	resources := map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricMemory): {
			Min: strPtr("512Mi"),
			Max: strPtr("16Gi"),
		},
	}
	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricMemory: float64(2 * 1024 * 1024 * 1024),
		},
		PodCount: 2,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Resources: resources,
	}

	got := ComputeTargets(in)
	if got.Memory == nil {
		t.Fatal("expected memory target")
	}
	// 2Gi * 1.2 / 2 pods = ~1.2Gi, rounded up to 2Gi
	want := resource.MustParse("2Gi")
	if got.Memory.Cmp(want) != 0 {
		t.Fatalf("memory target = %s, want %s", got.Memory.String(), want.String())
	}
	if got.Memory.String() != "2Gi" {
		t.Fatalf("memory target format = %q, want human-readable Mi/Gi not raw bytes", got.Memory.String())
	}
}

func TestNormalizeMemoryQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "raw bytes to Mi ceil",
			input: "229731402",
			want:  "220Mi",
		},
		{
			name:  "exact Mi unchanged",
			input: "512Mi",
			want:  "512Mi",
		},
		{
			name:  "exact Gi unchanged",
			input: "1Gi",
			want:  "1Gi",
		},
		{
			name:  "sub Mi rounds up to Mi",
			input: "1048577",
			want:  "2Mi",
		},
		{
			name:  "above 1024Mi uses Gi",
			input: "1288490189",
			want:  "2Gi",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeMemoryQuantity(resource.MustParse(tt.input))
			want := resource.MustParse(tt.want)
			if got.Cmp(want) != 0 {
				t.Fatalf("normalizeMemoryQuantity(%s) = %s, want %s", tt.input, got.String(), want.String())
			}
			if got.Cmp(resource.MustParse(tt.input)) < 0 {
				t.Fatalf("normalized value %s is less than input %s", got.String(), tt.input)
			}
		})
	}
}

func TestComputeTargetsMemoryNormalizedAfterClamp(t *testing.T) {
	t.Parallel()

	resources := map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricMemory): {
			Min: strPtr("128Mi"),
			Max: strPtr("16Gi"),
		},
	}
	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricMemory: 191442835,
		},
		PodCount: 1,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Resources: resources,
	}

	got := ComputeTargets(in)
	if got.Memory == nil {
		t.Fatal("expected memory target")
	}
	if got.Memory.String() == "229731402" {
		t.Fatal("memory target still shows raw bytes")
	}
	want := resource.MustParse("220Mi")
	if got.Memory.Cmp(want) != 0 {
		t.Fatalf("memory target = %s, want %s", got.Memory.String(), want.String())
	}
}

func TestComputeTargetsCPUWithObservedPeakFloor(t *testing.T) {
	t.Parallel()

	resources := resizeResourcesFixture()
	// Chronos median forecast for bursty CPU can stay near the duty-cycle average.
	forecastPeak := 400.0
	observedPeak := RecentPeak(burstyCPUSeriesFixture(), DefaultRecentPeakWindow)
	effective := EffectivePeak(forecastPeak, observedPeak)

	in := TargetInput{
		ForecastPeaks: map[autoscalingv1alpha1.ResourceMetric]float64{
			autoscalingv1alpha1.ResourceMetricCPU: effective,
		},
		PodCount: 2,
		CurrentRequests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("100m"),
		},
		Resources: resources,
	}

	got := ComputeTargets(in)
	want := resource.MustParse("1200m") // 2000m * 1.2 / 2 pods
	if got.CPU.Cmp(want) != 0 {
		t.Fatalf("CPU target = %s, want %s (effective peak %.0fm)", got.CPU.String(), want.String(), effective)
	}
}

func burstyCPUSeriesFixture() []float64 {
	out := make([]float64, 0, 120)
	for range 30 {
		out = append(out, 2000, 2000, 2000, 200)
	}
	return out
}

func TestForecastPeaks(t *testing.T) {
	t.Parallel()

	peak := ForecastPeaks([]float64{10, 50, 30}, []QuantileSeries{
		{Values: []float64{40, 80}},
	})
	if peak != 80 {
		t.Fatalf("peak = %v, want 80", peak)
	}
}

func resizeResourcesFixture() map[string]autoscalingv1alpha1.ResourceBoundsSpec {
	return map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricCPU): {
			Min: strPtr("250m"),
			Max: strPtr("8"),
		},
		string(autoscalingv1alpha1.ResourceMetricMemory): {
			Min: strPtr("512Mi"),
			Max: strPtr("16Gi"),
		},
	}
}
