package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	v1alpha1 	"github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/metrics"
)

type stubForecaster struct{}

func (stubForecaster) Forecast(context.Context, forecast.Request) (forecast.Response, error) {
	return forecast.Response{}, nil
}

func (stubForecaster) Close() error { return nil }

func TestForecastNotReadyReason(t *testing.T) {
	t.Parallel()

	horizon := int32(12)
	forecastSpec := &v1alpha1.ForecastSpec{Horizon: &horizon}
	resources := []v1alpha1.ResourceMetric{v1alpha1.ResourceMetricCPU}
	history := map[v1alpha1.ResourceMetric]metrics.Series{
		v1alpha1.ResourceMetricCPU: {Values: make([]float64, 8)},
	}
	fullHistory := map[v1alpha1.ResourceMetric]metrics.Series{
		v1alpha1.ResourceMetricCPU: {Values: make([]float64, metrics.MinForecastSamples)},
	}
	stubForecaster := stubForecaster{}

	t.Run("forecast not configured", func(t *testing.T) {
		na := &v1alpha1.NeuralAutoscaler{Spec: v1alpha1.NeuralAutoscalerSpec{}}
		reason, extra := forecastNotReadyReason(na, stubForecaster, resources, history, nil)
		if reason != "forecast_not_configured" || len(extra) != 0 {
			t.Fatalf("got reason=%q extra=%v", reason, extra)
		}
	})

	t.Run("forecaster not loaded", func(t *testing.T) {
		na := &v1alpha1.NeuralAutoscaler{Spec: v1alpha1.NeuralAutoscalerSpec{Forecast: forecastSpec}}
		reason, extra := forecastNotReadyReason(na, nil, resources, history, nil)
		if reason != "forecaster_not_loaded" || len(extra) != 0 {
			t.Fatalf("got reason=%q extra=%v", reason, extra)
		}
	})

	t.Run("insufficient history", func(t *testing.T) {
		na := &v1alpha1.NeuralAutoscaler{Spec: v1alpha1.NeuralAutoscalerSpec{Forecast: forecastSpec}}
		reason, extra := forecastNotReadyReason(na, stubForecaster, resources, history, nil)
		if reason != "insufficient_history" {
			t.Fatalf("reason = %q, want insufficient_history", reason)
		}
		if len(extra) != 4 || extra[0] != "samples" || extra[1] != 8 || extra[2] != "need" || extra[3] != metrics.MinForecastSamples {
			t.Fatalf("extra = %v", extra)
		}
	})

	t.Run("forecast failed", func(t *testing.T) {
		na := &v1alpha1.NeuralAutoscaler{Spec: v1alpha1.NeuralAutoscalerSpec{Forecast: forecastSpec}}
		errs := map[v1alpha1.ResourceMetric]error{
			v1alpha1.ResourceMetricCPU: errors.New("onnx inference failed"),
		}
		reason, extra := forecastNotReadyReason(na, stubForecaster, resources, fullHistory, errs)
		if reason != "forecast_failed" {
			t.Fatalf("reason = %q, want forecast_failed", reason)
		}
		if len(extra) != 4 || extra[0] != "resource" || extra[2] != "err" {
			t.Fatalf("extra = %v", extra)
		}
	})
}

func TestAppendHistorySamplePrometheusPath(t *testing.T) {
	t.Parallel()

	store := metrics.NewHistoryStore(512)
	r := &NeuralAutoscalerReconciler{
		MetricsFactory: &metrics.Factory{History: store},
	}
	key := metrics.HistoryKey("ns", "api", v1alpha1.ResourceMetricCPU)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	smoothedRange := metrics.Series{Values: []float64{200, 220, 240, 260, 280}}
	for i, latest := range []float64{200, 250, 1800} {
		snapshot := metrics.Series{
			Values:     append(append([]float64(nil), smoothedRange.Values...), latest),
			Timestamps: []time.Time{ts, ts.Add(time.Minute), ts.Add(2 * time.Minute), ts.Add(3 * time.Minute), ts.Add(4 * time.Minute), ts.Add(time.Duration(i+5) * time.Minute)},
		}
		r.appendHistorySample(key, snapshot)
	}

	recent := metrics.RecentPeakSamples(store, key, smoothedRange)
	if len(recent) != 3 {
		t.Fatalf("len(recent) = %d, want 3 reconcile-interval samples", len(recent))
	}
	if got := maxFloat64(recent); got != 1800 {
		t.Fatalf("recent peak samples max = %v, want 1800 (burst)", got)
	}
	if got := maxFloat64(smoothedRange.Values); got >= 1800 {
		t.Fatalf("smoothed range max = %v, should under-estimate burst", got)
	}
}

func TestAccumulateHistoryMetricsServerPath(t *testing.T) {
	t.Parallel()

	store := metrics.NewHistoryStore(512)
	r := &NeuralAutoscalerReconciler{
		MetricsFactory: &metrics.Factory{History: store},
	}
	key := metrics.HistoryKey("ns", "api", v1alpha1.ResourceMetricCPU)
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	snapshot := metrics.Series{
		Values:     []float64{100},
		Timestamps: []time.Time{ts},
	}
	got := r.accumulateHistory(key, snapshot)
	if len(got.Values) != 1 || got.Values[0] != 100 {
		t.Fatalf("first accumulate = %v, want [100]", got.Values)
	}

	snapshot = metrics.Series{
		Values:     []float64{500},
		Timestamps: []time.Time{ts.Add(time.Minute)},
	}
	got = r.accumulateHistory(key, snapshot)
	if len(got.Values) != 2 || got.Values[1] != 500 {
		t.Fatalf("second accumulate = %v, want [100 500]", got.Values)
	}
}

func maxFloat64(values []float64) float64 {
	var max float64
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}
