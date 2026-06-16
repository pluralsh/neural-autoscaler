package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestValidateMetricsSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    MetricsSourceSpec
		wantErr bool
	}{
		{
			name: "metrics server valid",
			spec: MetricsSourceSpec{
				Type: MetricsSourceMetricsServer,
				MetricsServer: &MetricsServerSourceSpec{
					TargetRef: CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
					Metric:    ResourceMetricCPU,
				},
			},
		},
		{
			name:    "metrics server missing config",
			spec:    MetricsSourceSpec{Type: MetricsSourceMetricsServer},
			wantErr: true,
		},
		{
			name: "prometheus valid range query",
			spec: MetricsSourceSpec{
				Type: MetricsSourcePrometheus,
				Prometheus: &PrometheusSourceSpec{
					URL:   "http://prometheus:9090",
					Query: `sum(rate(container_cpu_usage_seconds_total{namespace="default"}[5m]))`,
				},
			},
		},
		{
			name: "prometheus missing query",
			spec: MetricsSourceSpec{
				Type: MetricsSourcePrometheus,
				Prometheus: &PrometheusSourceSpec{
					URL: "http://prometheus:9090",
				},
			},
			wantErr: true,
		},
		{
			name: "prometheus invalid step",
			spec: MetricsSourceSpec{
				Type: MetricsSourcePrometheus,
				Prometheus: &PrometheusSourceSpec{
					URL:   "http://prometheus:9090",
					Query: "up",
					Step:  strPtr("not-a-duration"),
				},
			},
			wantErr: true,
		},
		{
			name: "prometheus auth missing name",
			spec: MetricsSourceSpec{
				Type: MetricsSourcePrometheus,
				Prometheus: &PrometheusSourceSpec{
					URL:   "http://prometheus:9090",
					Query: "up",
					Auth:  &corev1.SecretKeySelector{Key: "token"},
				},
			},
			wantErr: true,
		},
		{
			name: "prometheus auth missing key",
			spec: MetricsSourceSpec{
				Type: MetricsSourcePrometheus,
				Prometheus: &PrometheusSourceSpec{
					URL:   "http://prometheus:9090",
					Query: "up",
					Auth:  &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "prometheus-token"}},
				},
			},
			wantErr: true,
		},
		{
			name:    "unsupported type",
			spec:    MetricsSourceSpec{Type: "InfluxDB"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateMetricsSource(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateMetricsSource() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrometheusDefaultsAreOptional(t *testing.T) {
	t.Parallel()

	spec := PrometheusSourceSpec{
		URL:   "http://prometheus:9090",
		Query: "up",
	}
	if err := validatePrometheusSource(spec); err != nil {
		t.Fatalf("validatePrometheusSource() error = %v", err)
	}
}
