package onnx

import (
	"fmt"
	"strings"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

const (
	optionModelFamily = "model_family"

	ModelFamilyTimesFM   = "timesfm"
	ModelFamilyChronos2  = "chronos2"
)

// New constructs an ONNX forecaster for the configured model family.
func New(cfg forecast.Config) (forecast.Forecaster, error) {
	onnxCfg, err := ConfigFromForecast(cfg)
	if err != nil {
		return nil, err
	}
	return NewFromConfig(onnxCfg)
}

// NewFromConfig constructs an ONNX forecaster from a resolved onnx.Config.
func NewFromConfig(onnxCfg Config) (forecast.Forecaster, error) {
	switch onnxCfg.ModelFamily {
	case ModelFamilyTimesFM:
		return newTimesFM(onnxCfg)
	case ModelFamilyChronos2:
		return newChronos2(onnxCfg)
	default:
		return nil, fmt.Errorf("%w: unsupported %q %q (use %q or %q)",
			forecast.ErrInvalidRequest, optionModelFamily, onnxCfg.ModelFamily,
			ModelFamilyTimesFM, ModelFamilyChronos2)
	}
}

func parseModelFamily(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModelFamilyTimesFM:
		return ModelFamilyTimesFM
	case ModelFamilyChronos2, "chronos", "chronos-2", "chronos2-onnx":
		return ModelFamilyChronos2
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}
