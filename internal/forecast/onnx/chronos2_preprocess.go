package onnx

import (
	"math"
)

const (
	chronosContextLen           = 512
	chronosHorizonLen           = 64
	chronosFutureCovLen         = 64
	chronosQuantileCount        = 21
	defaultChronosOutputPatches = 4

	chronosModelVersion = "TSFM-ai/chronos-2-onnx"
)

// Chronos-2 quantile levels from amazon/chronos-2 config (21 levels).
var chronosQuantileLevels = []float64{
	0.01, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45,
	0.5, 0.55, 0.6, 0.65, 0.7, 0.75, 0.8, 0.85, 0.9, 0.95, 0.99,
}

type chronosInputs struct {
	context          []float32
	attentionMask    []float32
	futureCovariates []float32
	scale            float64
}

func prepareChronosInputs(values []float64) chronosInputs {
	tail := values
	if len(tail) > chronosContextLen {
		tail = tail[len(tail)-chronosContextLen:]
	}

	scale := medianAbsScale(tail)
	if scale == 0 {
		scale = 1
	}

	context := make([]float32, chronosContextLen)
	mask := make([]float32, chronosContextLen)
	offset := chronosContextLen - len(tail)
	for i, v := range tail {
		context[offset+i] = float32(math.Asinh(v / scale))
		mask[offset+i] = 1
	}

	futureCov := make([]float32, chronosFutureCovLen)

	return chronosInputs{
		context:          context,
		attentionMask:    mask,
		futureCovariates: futureCov,
		scale:            scale,
	}
}

func medianAbsScale(values []float64) float64 {
	if len(values) == 0 {
		return 1
	}

	abs := make([]float64, len(values))
	for i, v := range values {
		abs[i] = math.Abs(v)
	}

	for i := 1; i < len(abs); i++ {
		key := abs[i]
		j := i - 1
		for j >= 0 && abs[j] > key {
			abs[j+1] = abs[j]
			j--
		}
		abs[j+1] = key
	}

	mid := len(abs) / 2
	if len(abs)%2 == 0 {
		return (abs[mid-1] + abs[mid]) / 2
	}
	return abs[mid]
}

func chronosQuantileIndex(level float64) int {
	bestIdx := 0
	bestDist := math.MaxFloat64
	for i, q := range chronosQuantileLevels {
		dist := math.Abs(q - level)
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

func chronosPointSeries(preds []float32, scale float64, horizon int) []float64 {
	return chronosQuantileHorizon(preds, chronosQuantileIndex(0.5), scale, horizon)
}

func chronosQuantileHorizon(preds []float32, quantileIdx int, scale float64, horizon int) []float64 {
	if quantileIdx < 0 || quantileIdx >= chronosQuantileCount {
		return nil
	}
	if horizon <= 0 {
		return nil
	}
	if horizon > chronosHorizonLen {
		horizon = chronosHorizonLen
	}

	out := make([]float64, horizon)
	for step := range horizon {
		offset := quantileIdx*chronosHorizonLen + step
		if offset >= len(preds) {
			break
		}
		out[step] = math.Sinh(float64(preds[offset])) * scale
	}
	return out
}
