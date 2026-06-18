package onnx

import (
	"math"
	"testing"
)

func TestPrepareChronosInputs(t *testing.T) {
	t.Parallel()

	got := prepareChronosInputs([]float64{10, 20, 30})
	if len(got.context) != chronosContextLen {
		t.Fatalf("expected context length %d, got %d", chronosContextLen, len(got.context))
	}
	if got.attentionMask[chronosContextLen-3] != 1 || got.attentionMask[0] != 0 {
		t.Fatalf("unexpected attention mask tail: %v", got.attentionMask[:5])
	}
	if got.context[chronosContextLen-1] != float32(math.Asinh(30/got.scale)) {
		t.Fatalf("unexpected transformed context tail: %f", got.context[chronosContextLen-1])
	}
}

func TestChronosQuantileIndex(t *testing.T) {
	t.Parallel()

	if got := chronosQuantileIndex(0.9); got != 18 {
		t.Fatalf("expected index 18 for p90, got %d", got)
	}
	if got := chronosQuantileIndex(0.5); got != 10 {
		t.Fatalf("expected index 10 for median, got %d", got)
	}
}

func TestChronosQuantileHorizon(t *testing.T) {
	t.Parallel()

	preds := make([]float32, chronosQuantileCount*chronosHorizonLen)
	preds[10*chronosHorizonLen] = float32(math.Asinh(42)) // median, step 0

	got := chronosQuantileHorizon(preds, 10, 1, 1)
	if len(got) != 1 || math.Abs(got[0]-42) > 1e-4 {
		t.Fatalf("unexpected median horizon: %v", got)
	}
}
