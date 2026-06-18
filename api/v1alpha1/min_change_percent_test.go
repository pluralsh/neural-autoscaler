package v1alpha1

import "testing"

func TestEffectiveMinChangePercent(t *testing.T) {
	t.Parallel()

	global := int32(10)
	perResource := int32(20)

	tests := []struct {
		name        string
		global      *int32
		perResource *int32
		want        int32
	}{
		{
			name:        "per-resource override",
			global:      &global,
			perResource: &perResource,
			want:        20,
		},
		{
			name:   "global value",
			global: &global,
			want:   10,
		},
		{
			name: "default when unset",
			want: DefaultMinChangePercent,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EffectiveMinChangePercent(tt.global, tt.perResource)
			if got != tt.want {
				t.Fatalf("EffectiveMinChangePercent() = %d, want %d", got, tt.want)
			}
		})
	}
}
