package onnx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
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

func TestDiscoverModelFamilyWithRealModels(t *testing.T) {
	root := filepath.Join("..", "..", "..", "models")

	timesfmPath := filepath.Join(root, "timesfm-2.5-onnx", "onnx", "model.onnx")
	if _, err := os.Stat(timesfmPath); err == nil {
		t.Run("timesfm", func(t *testing.T) {
			family, err := DiscoverModelFamily(timesfmPath, "", defaultORTAPIVersion)
			if err != nil {
				t.Skipf("ONNX runtime unavailable: %v", err)
			}
			if family != ModelFamilyTimesFM {
				t.Fatalf("DiscoverModelFamily(timesfm) = %q, want %q", family, ModelFamilyTimesFM)
			}
		})
	}

	chronosPath := filepath.Join(root, "chronos-2-onnx", "model.onnx")
	if _, err := os.Stat(chronosPath); err == nil {
		t.Run("chronos2", func(t *testing.T) {
			family, err := DiscoverModelFamily(chronosPath, "", defaultORTAPIVersion)
			if err != nil {
				t.Skipf("ONNX runtime unavailable: %v", err)
			}
			if family != ModelFamilyChronos2 {
				t.Fatalf("DiscoverModelFamily(chronos2) = %q, want %q", family, ModelFamilyChronos2)
			}
		})
	}
}

func TestConfigFromForecastDiscoversFamily(t *testing.T) {
	timesfmPath := filepath.Join("..", "..", "..", "models", "timesfm-2.5-onnx", "onnx", "model.onnx")
	if _, err := os.Stat(timesfmPath); err != nil {
		t.Skip("timesfm model not present")
	}

	cfg, err := ConfigFromForecast(forecast.Config{
		Options: map[string]string{
			"model_path": timesfmPath,
		},
	})
	if err != nil {
		t.Skipf("ONNX runtime unavailable: %v", err)
	}
	if cfg.ModelFamily != ModelFamilyTimesFM {
		t.Fatalf("ModelFamily = %q, want %q", cfg.ModelFamily, ModelFamilyTimesFM)
	}
}
