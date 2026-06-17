package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ValidateResize performs semantic validation for in-place resize configuration.
func ValidateResize(spec *ResizeSpec) error {
	if spec == nil {
		return nil
	}

	if err := validateMinChangePercent("resize.minChangePercent", spec.MinChangePercent); err != nil {
		return err
	}

	return validateResizeResources(spec.Resources)
}

func validateMinChangePercent(field string, value *int32) error {
	if value == nil {
		return nil
	}
	if *value < 0 || *value > 100 {
		return fmt.Errorf("%s must be between 0 and 100", field)
	}
	return nil
}

func validateResizeResources(resources map[string]ResourceBoundsSpec) error {
	if len(resources) == 0 {
		return fmt.Errorf("resize.resources must contain at least one resource")
	}
	for key, bounds := range resources {
		if err := validateResourceKey(key); err != nil {
			return fmt.Errorf("resize.resources: %w", err)
		}
		if err := validateResourceBoundsSpec(key, bounds); err != nil {
			return fmt.Errorf("resize.resources.%s: %w", key, err)
		}
	}
	return nil
}

func validateResourceKey(key string) error {
	switch ResourceMetric(key) {
	case ResourceMetricCPU, ResourceMetricMemory:
		return nil
	default:
		return fmt.Errorf("unsupported resource %q", key)
	}
}

func validateResourceBoundsSpec(key string, bounds ResourceBoundsSpec) error {
	minQ, err := parseOptionalQuantity(bounds.Min)
	if err != nil {
		return fmt.Errorf("min: %w", err)
	}
	maxQ, err := parseOptionalQuantity(bounds.Max)
	if err != nil {
		return fmt.Errorf("max: %w", err)
	}
	if minQ != nil && maxQ != nil && minQ.Cmp(*maxQ) > 0 {
		return fmt.Errorf("min (%s) exceeds max (%s)", minQ.String(), maxQ.String())
	}
	if err := validateMinChangePercent("minChangePercent", bounds.MinChangePercent); err != nil {
		return err
	}
	_ = key
	return nil
}

func parseOptionalQuantity(raw *string) (*resource.Quantity, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	q, err := resource.ParseQuantity(*raw)
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// ControlledResourceSet returns the controlled resources as a set of corev1.ResourceName values.
func ControlledResourceSet(resources map[string]ResourceBoundsSpec) map[corev1.ResourceName]bool {
	out := make(map[corev1.ResourceName]bool, len(resources))
	for key := range resources {
		switch ResourceMetric(key) {
		case ResourceMetricCPU:
			out[corev1.ResourceCPU] = true
		case ResourceMetricMemory:
			out[corev1.ResourceMemory] = true
		}
	}
	return out
}

// ResourceBounds returns parsed min/max quantities for a resource entry.
func ResourceBounds(bounds ResourceBoundsSpec) (minQ, maxQ *resource.Quantity, err error) {
	minQ, err = parseOptionalQuantity(bounds.Min)
	if err != nil {
		return nil, nil, fmt.Errorf("min: %w", err)
	}
	maxQ, err = parseOptionalQuantity(bounds.Max)
	if err != nil {
		return nil, nil, fmt.Errorf("max: %w", err)
	}
	return minQ, maxQ, nil
}
