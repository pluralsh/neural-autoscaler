package onnx

import (
	"testing"
)

func TestFamilyFromInputNames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		names []string
		want  string
		ok    bool
	}{
		{[]string{"past_values", "mean_predictions"}, ModelFamilyTimesFM, true},
		{[]string{"context", "attention_mask", "future_covariates"}, ModelFamilyChronos2, true},
		{[]string{"context"}, ModelFamilyChronos2, true},
		{[]string{"foo", "bar"}, "", false},
		{nil, "", false},
	}

	for _, tc := range cases {
		got, ok := familyFromInputNames(tc.names)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("familyFromInputNames(%v) = (%q, %v), want (%q, %v)", tc.names, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFamilyFromModelPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/models/timesfm-2.5-onnx/onnx/model.onnx", ModelFamilyTimesFM, true},
		{"/models/chronos-2-onnx/model.onnx", ModelFamilyChronos2, true},
		{"/models/CHRONOS-2/model.onnx", ModelFamilyChronos2, true},
		{"/opt/models/generic/model.onnx", "", false},
	}

	for _, tc := range cases {
		got, ok := familyFromModelPath(tc.path)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("familyFromModelPath(%q) = (%q, %v), want (%q, %v)", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseModelFamily(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":          "",
		"timesfm":   ModelFamilyTimesFM,
		"chronos2":  ModelFamilyChronos2,
		"chronos-2": ModelFamilyChronos2,
	}
	for raw, want := range cases {
		if got := parseModelFamily(raw); got != want {
			t.Fatalf("parseModelFamily(%q) = %q, want %q", raw, got, want)
		}
	}
}
