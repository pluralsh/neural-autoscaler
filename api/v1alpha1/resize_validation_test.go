package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func int32Ptr(v int32) *int32 { return &v }

func TestValidateResize(t *testing.T) {
	t.Parallel()

	valid := &ResizeSpec{
		Resources: map[string]ResourceBoundsSpec{
			string(ResourceMetricCPU): {
				Min: strPtr("250m"),
				Max: strPtr("8"),
			},
			string(ResourceMetricMemory): {
				Min: strPtr("512Mi"),
				Max: strPtr("16Gi"),
			},
		},
	}

	if err := ValidateResize(valid); err != nil {
		t.Fatalf("ValidateResize() error = %v", err)
	}

	tests := []struct {
		name    string
		spec    *ResizeSpec
		wantErr bool
	}{
		{
			name:    "nil spec",
			spec:    nil,
			wantErr: false,
		},
		{
			name: "no resources",
			spec: &ResizeSpec{
				Resources: map[string]ResourceBoundsSpec{},
			},
			wantErr: true,
		},
		{
			name: "min exceeds max cpu",
			spec: &ResizeSpec{
				Resources: map[string]ResourceBoundsSpec{
					string(ResourceMetricCPU): {
						Min: strPtr("2"),
						Max: strPtr("1"),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "unsupported resource",
			spec: &ResizeSpec{
				Resources: map[string]ResourceBoundsSpec{
					"ephemeral-storage": {},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid min quantity",
			spec: &ResizeSpec{
				Resources: map[string]ResourceBoundsSpec{
					string(ResourceMetricCPU): {Min: strPtr("not-a-quantity")},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid global minChangePercent",
			spec: &ResizeSpec{
				MinChangePercent: int32Ptr(101),
				Resources: map[string]ResourceBoundsSpec{
					string(ResourceMetricCPU): {},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid per-resource minChangePercent",
			spec: &ResizeSpec{
				Resources: map[string]ResourceBoundsSpec{
					string(ResourceMetricCPU): {MinChangePercent: int32Ptr(-1)},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateResize(tt.spec)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateResize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPolicyClamp(t *testing.T) {
	t.Parallel()

	bounds := ResourceBoundsSpec{
		Min: strPtr("250m"),
		Max: strPtr("8"),
	}

	cpu := resource.MustParse("100m")
	clamped := ClampQuantity(cpu, bounds)
	if clamped.Cmp(resource.MustParse("250m")) != 0 {
		t.Fatalf("cpu min clamp: got %s", clamped.String())
	}

	cpu = resource.MustParse("16")
	clamped = ClampQuantity(cpu, bounds)
	if clamped.Cmp(resource.MustParse("8")) != 0 {
		t.Fatalf("cpu max clamp: got %s", clamped.String())
	}
}
