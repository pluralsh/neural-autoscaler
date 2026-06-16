package onnx

func prepareContext(values []float64, maxContext int) []float32 {
	if maxContext <= 0 {
		maxContext = len(values)
	}

	start := 0
	if len(values) > maxContext {
		start = len(values) - maxContext
	}

	out := make([]float32, len(values[start:]))
	for i, v := range values[start:] {
		out[i] = float32(v)
	}
	return out
}

// prepareBatchInput builds a [batchSize, contextLen] row-major tensor for ONNX.
// Some TimesFM exports fix batch size at export time (commonly 2); extra rows
// duplicate the same series so single-series forecasts still work.
func prepareBatchInput(values []float64, maxContext, batchSize int) ([]float32, int) {
	row := prepareContext(values, maxContext)
	if batchSize < 1 {
		batchSize = 1
	}

	contextLen := len(row)
	out := make([]float32, batchSize*contextLen)
	for b := range batchSize {
		copy(out[b*contextLen:(b+1)*contextLen], row)
	}
	return out, contextLen
}

func sliceHorizon(values []float32, horizon int) []float64 {
	if horizon <= 0 {
		return nil
	}
	if len(values) < horizon {
		horizon = len(values)
	}

	out := make([]float64, horizon)
	for i := range horizon {
		out[i] = float64(values[i])
	}
	return out
}

// quantileIndex maps common quantile levels to TimesFM full_predictions indices.
// Index 0 is mean; indices 1-9 are p10 through p90 in the PyTorch export.
func quantileIndex(level float64) int {
	switch {
	case level <= 0.15:
		return 1
	case level <= 0.25:
		return 2
	case level <= 0.35:
		return 3
	case level <= 0.45:
		return 4
	case level <= 0.55:
		return 5
	case level <= 0.65:
		return 6
	case level <= 0.75:
		return 7
	case level <= 0.85:
		return 8
	default:
		return 9
	}
}

func quantileSeries(full []float32, shape []int64, level float64, horizon int) []float64 {
	if len(shape) != 3 || len(full) == 0 {
		return nil
	}

	horizonLen := int(shape[1])
	quantileCount := int(shape[2])
	if horizonLen == 0 || quantileCount == 0 {
		return nil
	}

	idx := quantileIndex(level)
	if idx >= quantileCount {
		idx = quantileCount - 1
	}
	if horizon > horizonLen {
		horizon = horizonLen
	}

	out := make([]float64, horizon)
	for step := range horizon {
		offset := step*quantileCount + idx
		if offset >= len(full) {
			break
		}
		out[step] = float64(full[offset])
	}
	return out
}
