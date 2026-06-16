package onnx

import (
	"testing"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

func TestPrepareBatchInput(t *testing.T) {
	t.Parallel()

	data, contextLen := prepareBatchInput([]float64{1, 2, 3}, 2, 2)
	if contextLen != 2 {
		t.Fatalf("expected context length 2, got %d", contextLen)
	}
	if len(data) != 4 {
		t.Fatalf("expected 4 values, got %d", len(data))
	}
	if data[0] != 2 || data[1] != 3 || data[2] != 2 || data[3] != 3 {
		t.Fatalf("unexpected batch tensor: %v", data)
	}
}

func TestPrepareContext(t *testing.T) {
	t.Parallel()

	got := prepareContext([]float64{1, 2, 3, 4, 5}, 3)
	if len(got) != 3 {
		t.Fatalf("expected 3 values, got %d", len(got))
	}
	if got[0] != 3 || got[1] != 4 || got[2] != 5 {
		t.Fatalf("unexpected tail slice: %v", got)
	}
}

func TestSliceHorizon(t *testing.T) {
	t.Parallel()

	got := sliceHorizon([]float32{1, 2, 3, 4}, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("unexpected horizon slice: %v", got)
	}
}

func TestQuantileSeries(t *testing.T) {
	t.Parallel()

	full := []float32{
		0, 1, 2,
		10, 11, 12,
	}
	shape := []int64{1, 2, 3}
	got := quantileSeries(full, shape, 0.9, 2)
	if len(got) != 2 || got[0] != 2 || got[1] != 12 {
		t.Fatalf("unexpected quantile series: %v", got)
	}
}

func TestConfigFromForecastRequiresModelPath(t *testing.T) {
	t.Parallel()

	_, err := ConfigFromForecast(forecast.Config{
		Options: map[string]string{
			"model_path": "",
		},
	})
	if err == nil {
		t.Fatal("expected error for missing model path")
	}
}
