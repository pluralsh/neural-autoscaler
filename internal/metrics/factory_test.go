package metrics

import (
	"strings"
	"testing"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

func TestFactoryNewFetcherPrometheusNotImplemented(t *testing.T) {
	t.Parallel()

	factory := &Factory{}
	spec := autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourcePrometheus,
		Prometheus: &autoscalingv1alpha1.PrometheusSourceSpec{
			URL:   "http://prometheus:9090",
			Query: "up",
		},
	}

	_, err := factory.NewFetcher(spec, "default")
	if err == nil {
		t.Fatal("expected error for Prometheus metrics source")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("NewFetcher() error = %v, want not implemented", err)
	}
}

func TestFactoryNewFetcherMetricsServerRequiresClients(t *testing.T) {
	t.Parallel()

	factory := &Factory{}
	_, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourceMetricsServer,
		MetricsServer: &autoscalingv1alpha1.MetricsServerSourceSpec{
			TargetRef: autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Pod", Name: "api-1"},
			Metric:    autoscalingv1alpha1.ResourceMetricCPU,
		},
	}, "default")
	if err == nil {
		t.Fatal("expected error when metrics server clients are missing")
	}
}
