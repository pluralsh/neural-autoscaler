package metrics

import (
	"fmt"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

const containerFilter = `container!="",container!="POD"`

// BuildPrometheusQuery returns a PromQL expression for the given resource and pod matcher.
func BuildPrometheusQuery(namespace string, resource autoscalingv1alpha1.ResourceMetric, podMatcher string, exactPod bool) (string, error) {
	podLabel := podLabelSelector(podMatcher, exactPod)
	ns := escapePromQLString(namespace)

	switch resource {
	case autoscalingv1alpha1.ResourceMetricCPU:
		return fmt.Sprintf(
			`sum(rate(container_cpu_usage_seconds_total{namespace=%q,%s,%s}[5m])) * 1000`,
			ns, podLabel, containerFilter,
		), nil
	case autoscalingv1alpha1.ResourceMetricMemory:
		return fmt.Sprintf(
			`sum(container_memory_working_set_bytes{namespace=%q,%s,%s})`,
			ns, podLabel, containerFilter,
		), nil
	default:
		return "", fmt.Errorf("unsupported resource %q", resource)
	}
}

func podLabelSelector(matcher string, exact bool) string {
	if exact {
		return fmt.Sprintf(`pod=%q`, escapePromQLString(matcher))
	}
	return fmt.Sprintf(`pod=~%q`, escapePromQLString(matcher))
}

func escapePromQLString(s string) string {
	return s
}
