package v1alpha1

import (
	"testing"
	"time"
)

func TestSetDuration(t *testing.T) {
	t.Parallel()

	minute := "1m"
	invalid := "not-a-duration"

	tests := []struct {
		name    string
		input   *string
		want    *time.Duration
		wantErr bool
	}{
		{name: "nil", input: nil, want: nil},
		{name: "empty", input: strPtr(""), want: nil},
		{name: "blank", input: strPtr("   "), want: nil},
		{name: "valid", input: &minute, want: durationPtr(time.Minute)},
		{name: "invalid", input: &invalid, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := SetDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SetDuration() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("SetDuration() = %v, want nil", got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("SetDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func durationPtr(d time.Duration) *time.Duration {
	return &d
}
