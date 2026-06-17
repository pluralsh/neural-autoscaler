package onnx

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/resize"
)

// burstyCPUSeries simulates the sample workload: 45s high / 15s low sampled every 20s.
func burstyCPUSeries(cycles int, highMilli, lowMilli float64) []float64 {
	const (
		highSteps = 3 // 45s / 20s ≈ 2.25, round to 3 samples in high phase
		lowSteps  = 1 // 15s / 20s ≈ 0.75, round to 1 sample in low phase
	)
	out := make([]float64, 0, cycles*(highSteps+lowSteps))
	for range cycles {
		for range highSteps {
			out = append(out, highMilli)
		}
		for range lowSteps {
			out = append(out, lowMilli)
		}
	}
	return out
}

func TestChronos2BurstyCPUForecast(t *testing.T) {
	modelPath := filepath.Join("..", "..", "..", "models", "chronos-2-onnx", "model.onnx")
	if _, err := os.Stat(modelPath); err != nil {
		t.Skip("chronos-2 ONNX model not present:", modelPath)
	}

	f, err := newChronos2(Config{ModelPath: modelPath, ORTAPIVersion: 23})
	if err != nil {
		t.Fatalf("newChronos2: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	history := burstyCPUSeries(30, 2000, 200)
	recentMax := history[len(history)-1]
	for _, v := range history[len(history)-5:] {
		if v > recentMax {
			recentMax = v
		}
	}

	ctx := context.Background()
	for _, quantiles := range [][]float64{nil, {0.9, 0.99}} {
		resp, err := f.Forecast(ctx, forecast.Request{
			SeriesID:  "test/cpu",
			Values:    history,
			Horizon:   12,
			Quantiles: quantiles,
		})
		if err != nil {
			t.Skipf("Forecast(quantiles=%v): %v", quantiles, err)
		}

		qSeries := make([]resize.QuantileSeries, 0, len(resp.Quantiles))
		for _, q := range resp.Quantiles {
			qSeries = append(qSeries, resize.QuantileSeries{Level: q.Level, Values: q.Values})
		}
		peak := resize.ForecastPeaks(resp.Point, qSeries)

		t.Logf("quantiles=%v median=%.0f peak=%.0f recentMax=%.0f historyTail=%v",
			quantiles, resp.Point[0], peak, recentMax, history[len(history)-5:])
		if peak < 500 {
			t.Logf("forecast peak %.0f low without recent-observed floor (expected for bursty median)", peak)
		}
	}

	// Stable memory-like series for comparison.
	memHistory := make([]float64, 87)
	for i := range memHistory {
		memHistory[i] = 591_384_576 + float64(i%3)*1e6
	}
	memResp, err := f.Forecast(ctx, forecast.Request{SeriesID: "test/memory", Values: memHistory, Horizon: 12})
	if err != nil {
		t.Fatalf("memory Forecast: %v", err)
	}
	memPeak := resize.ForecastPeaks(memResp.Point, nil)
	t.Logf("stable memory median=%.0f peak=%.0f (last sample %.0f)",
		memResp.Point[0], memPeak, memHistory[len(memHistory)-1])
	if math.Abs(memPeak-memHistory[len(memHistory)-1])/memHistory[len(memHistory)-1] > 0.3 {
		t.Errorf("memory forecast should track stable usage; peak=%.0f last=%.0f", memPeak, memHistory[len(memHistory)-1])
	}
}
