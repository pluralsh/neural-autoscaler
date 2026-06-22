package v1alpha1

import (
	"fmt"
	"strings"
)

// ValidateMetricsSource performs lightweight semantic validation beyond CRD schema checks.
func ValidateMetricsSource(spec MetricsSourceSpec) error {
	switch spec.Type {
	case MetricsSourceMetricsServer:
		if spec.MetricsServer == nil {
			return fmt.Errorf("metricsServer configuration is required when type is MetricsServer")
		}
		return validateMetricsServerSource(*spec.MetricsServer)
	case MetricsSourcePrometheus:
		if spec.Prometheus == nil {
			return fmt.Errorf("prometheus configuration is required when type is Prometheus")
		}
		return validatePrometheusSource(*spec.Prometheus)
	default:
		return fmt.Errorf("unsupported metrics source type %q", spec.Type)
	}
}

func validateMetricsServerSource(spec MetricsServerSourceSpec) error {
	ref := spec.TargetRef
	if strings.TrimSpace(ref.Kind) == "" {
		return fmt.Errorf("metricsServer.targetRef.kind is required")
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("metricsServer.targetRef.name is required")
	}
	switch ref.Kind {
	case "Deployment", "StatefulSet", "ReplicaSet", "Pod":
	default:
		return fmt.Errorf("metricsServer.targetRef.kind %q is not supported", ref.Kind)
	}
	if len(spec.Resources) == 0 {
		return fmt.Errorf("metricsServer.resources must contain at least one resource")
	}
	return validateResourceMetrics(spec.Resources, "metricsServer.resources")
}

func validatePrometheusSource(spec PrometheusSourceSpec) error {
	if strings.TrimSpace(spec.URL) == "" {
		return fmt.Errorf("prometheus.url is required")
	}

	hasQuery := strings.TrimSpace(spec.Query) != ""
	hasTargetRef := spec.TargetRef != nil

	if hasTargetRef {
		if err := validatePrometheusTargetRef(*spec.TargetRef); err != nil {
			return err
		}
		if len(spec.Resources) == 0 {
			return fmt.Errorf("prometheus.resources must contain at least one resource when targetRef is set")
		}
		if err := validateResourceMetrics(spec.Resources, "prometheus.resources"); err != nil {
			return err
		}
	} else if !hasQuery {
		return fmt.Errorf("prometheus.targetRef and prometheus.resources are required when query is unset")
	}

	switch spec.QueryType {
	case "", PrometheusQueryInstant, PrometheusQueryRange:
	default:
		return fmt.Errorf("prometheus.queryType %q is not supported", spec.QueryType)
	}
	if spec.Auth != nil {
		if strings.TrimSpace(spec.Auth.Name) == "" {
			return fmt.Errorf("prometheus.auth.name is required when auth is set")
		}
		if strings.TrimSpace(spec.Auth.Key) == "" {
			return fmt.Errorf("prometheus.auth.key is required when auth is set")
		}
	}
	if err := validateOptionalDuration("prometheus.step", spec.Step); err != nil {
		return err
	}
	if err := validateOptionalDuration("prometheus.lookback", spec.Lookback); err != nil {
		return err
	}
	return nil
}

func validatePrometheusTargetRef(ref CrossVersionObjectReference) error {
	if strings.TrimSpace(ref.Kind) == "" {
		return fmt.Errorf("prometheus.targetRef.kind is required")
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("prometheus.targetRef.name is required")
	}
	switch ref.Kind {
	case "Deployment", "StatefulSet", "ReplicaSet", "Pod":
	default:
		return fmt.Errorf("prometheus.targetRef.kind %q is not supported", ref.Kind)
	}
	return nil
}

func validateResourceMetrics(resources []ResourceMetric, field string) error {
	seen := make(map[ResourceMetric]struct{}, len(resources))
	for _, r := range resources {
		switch r {
		case ResourceMetricCPU, ResourceMetricMemory:
		default:
			return fmt.Errorf("%s: unsupported resource %q", field, r)
		}
		if _, ok := seen[r]; ok {
			return fmt.Errorf("%s: duplicate resource %q", field, r)
		}
		seen[r] = struct{}{}
	}
	return nil
}

func validateOptionalDuration(field string, interval *string) error {
	if _, err := SetDuration(interval); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

// ValidateForecast performs lightweight semantic validation for forecast settings.
func ValidateForecast(spec *ForecastSpec) error {
	if spec == nil {
		return nil
	}
	return validateOptionalDuration("forecast.step", spec.Step)
}
