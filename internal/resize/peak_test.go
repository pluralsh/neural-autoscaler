package resize

import "testing"

func TestRecentPeak(t *testing.T) {
	t.Parallel()

	values := []float64{10, 50, 30, 200, 40, 100}
	if got := RecentPeak(values, 3); got != 200 {
		t.Fatalf("RecentPeak(last 3) = %v, want 200", got)
	}
	if got := RecentPeak(values, 0); got != 200 {
		t.Fatalf("RecentPeak(default window) = %v, want 200", got)
	}
	if got := RecentPeak(nil, 5); got != 0 {
		t.Fatalf("RecentPeak(empty) = %v, want 0", got)
	}
}

func TestEffectivePeak(t *testing.T) {
	t.Parallel()

	if got := EffectivePeak(500, 2000); got != 2000 {
		t.Fatalf("EffectivePeak() = %v, want 2000", got)
	}
	if got := EffectivePeak(3000, 2000); got != 3000 {
		t.Fatalf("EffectivePeak() = %v, want 3000", got)
	}
}
