package metrics

import (
	"strings"
	"testing"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

func TestPodMatcher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		podNames []string
		ref      autoscalingv1alpha1.CrossVersionObjectReference
		want     string
		exact    bool
	}{
		{
			name:     "deployment pods joined",
			podNames: []string{"api-abc123", "api-def456"},
			ref:      autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			want:     "api-abc123|api-def456",
			exact:    false,
		},
		{
			name:     "statefulset fallback pattern",
			podNames: nil,
			ref:      autoscalingv1alpha1.CrossVersionObjectReference{Kind: "StatefulSet", Name: "postgres"},
			want:     "postgres-.*",
			exact:    false,
		},
		{
			name:     "single pod exact",
			podNames: []string{"api-1"},
			ref:      autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Pod", Name: "api-1"},
			want:     "api-1",
			exact:    true,
		},
		{
			name:     "deployment fallback pattern",
			podNames: nil,
			ref:      autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			want:     "api-.*",
			exact:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, exact := PodMatcher(tt.podNames, tt.ref)
			if got != tt.want || exact != tt.exact {
				t.Fatalf("PodMatcher() = (%q, %v), want (%q, %v)", got, exact, tt.want, tt.exact)
			}
		})
	}
}

func TestBuildPrometheusQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		namespace  string
		resource   autoscalingv1alpha1.ResourceMetric
		podMatcher string
		exactPod   bool
		contains   []string
	}{
		{
			name:       "cpu deployment pattern",
			namespace:  "default",
			resource:   autoscalingv1alpha1.ResourceMetricCPU,
			podMatcher: "api-.*",
			contains: []string{
				`namespace="default"`,
				`pod=~"api-.*"`,
				`container_cpu_usage_seconds_total`,
				`* 1000`,
			},
		},
		{
			name:       "memory joined pods",
			namespace:  "prod",
			resource:   autoscalingv1alpha1.ResourceMetricMemory,
			podMatcher: "api-1|api-2",
			contains: []string{
				`namespace="prod"`,
				`pod=~"api-1|api-2"`,
				`container_memory_working_set_bytes`,
			},
		},
		{
			name:       "cpu exact pod",
			namespace:  "default",
			resource:   autoscalingv1alpha1.ResourceMetricCPU,
			podMatcher: "api-1",
			exactPod:   true,
			contains: []string{
				`pod="api-1"`,
				`container_cpu_usage_seconds_total`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, err := BuildPrometheusQuery(tt.namespace, tt.resource, tt.podMatcher, tt.exactPod)
			if err != nil {
				t.Fatalf("BuildPrometheusQuery() error = %v", err)
			}
			for _, part := range tt.contains {
				if !strings.Contains(query, part) {
					t.Fatalf("query %q missing %q", query, part)
				}
			}
		})
	}
}
