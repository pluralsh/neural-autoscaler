package forecast

import (
	"strings"
	"testing"
	"time"
)

func TestFormatPoints(t *testing.T) {
	t.Parallel()

	step := time.Minute
	points := []float64{250, 1500, 450}

	got := FormatPoints(step, points, UnitMillicores)
	wantParts := []string{"+1m: 250m", "+2m: 1.50 cores", "+3m: 450m"}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("FormatPoints() = %q, missing %q", got, part)
		}
	}
}

func TestFormatValueMillicores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value float64
		want  string
	}{
		{250, "250m"},
		{999, "999m"},
		{1000, "1.00 cores"},
		{1500, "1.50 cores"},
		{12345, "12.3 cores"},
	}
	for _, tt := range tests {
		if got := FormatValue(tt.value, UnitMillicores); got != tt.want {
			t.Errorf("FormatValue(%v, UnitMillicores) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestFormatValueBytes(t *testing.T) {
	t.Parallel()

	if got := FormatValue(512, UnitBytes); got != "512 B" {
		t.Fatalf("FormatValue(512, UnitBytes) = %q, want 512 B", got)
	}
	if got := FormatValue(128*1024*1024, UnitBytes); !strings.Contains(got, "MiB") {
		t.Fatalf("FormatValue(memory, UnitBytes) = %q, want MiB suffix", got)
	}
}

func TestFormatQuantiles(t *testing.T) {
	t.Parallel()

	got := FormatQuantiles(time.Minute, []QuantileSeries{
		{Level: 0.1, Values: []float64{200}},
		{Level: 0.9, Values: []float64{300}},
	}, UnitMillicores)

	if !strings.Contains(got, "q0.10 (+1m: 200m)") {
		t.Fatalf("FormatQuantiles() = %q", got)
	}
	if !strings.Contains(got, "q0.90 (+1m: 300m)") {
		t.Fatalf("FormatQuantiles() = %q", got)
	}
}

func TestFormatStepOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		step  time.Duration
		index int
		want  string
	}{
		{time.Minute, 0, "+1m"},
		{time.Minute, 11, "+12m"},
		{time.Hour, 0, "+1h"},
		{30 * time.Second, 1, "+1m"},
	}
	for _, tt := range tests {
		if got := formatStepOffset(tt.step, tt.index); got != tt.want {
			t.Errorf("formatStepOffset(%v, %d) = %q, want %q", tt.step, tt.index, got, tt.want)
		}
	}
}
