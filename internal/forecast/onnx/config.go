package onnx

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

const (
	optionModelPath       = "model_path"
	optionMaxContext      = "max_context"
	optionRuntimeLibPath  = "runtime_lib_path"
	optionORTAPIVersion   = "ort_api_version"
	optionIntraOpThreads  = "intra_op_threads"
	optionInputName       = "input_name"
	optionMeanOutputName  = "mean_output_name"
	optionFullOutputName  = "full_output_name"
	optionBatchSize       = "batch_size"
	optionNumOutputPatches = "num_output_patches"

	defaultMaxContext     = 1024
	defaultORTAPIVersion  = 23
	defaultIntraOpThreads = 4
	defaultBatchSize      = 2

	inputPastValues       = "past_values"
	outputMeanPredictions = "mean_predictions"
	outputFullPredictions = "full_predictions"

	modelVersion = "google/timesfm-2.5-200m-transformers-onnx"
)

type Config struct {
	ModelFamily       string
	ModelPath         string
	MaxContext        int
	RuntimeLibPath    string
	ORTAPIVersion     int
	IntraOpThreads    int
	InputName         string
	MeanOutputName    string
	FullOutputName    string
	BatchSize         int
	NumOutputPatches  int
}

func ConfigFromForecast(cfg forecast.Config) (Config, error) {
	opts := cfg.Options
	if opts == nil {
		opts = map[string]string{}
	}

	modelPath := strings.TrimSpace(opts[optionModelPath])
	if modelPath == "" {
		return Config{}, fmt.Errorf("%w: %q option is required", forecast.ErrInvalidRequest, optionModelPath)
	}
	if _, err := os.Stat(modelPath); err != nil {
		return Config{}, fmt.Errorf("onnx model: %w", err)
	}

	modelFamily := parseModelFamily(opts[optionModelFamily])

	maxContext, err := parsePositiveInt(opts[optionMaxContext], defaultMaxContext)
	if err != nil {
		return Config{}, fmt.Errorf("parse %q: %w", optionMaxContext, err)
	}

	ortVersion, err := parsePositiveInt(opts[optionORTAPIVersion], defaultORTAPIVersion)
	if err != nil {
		return Config{}, fmt.Errorf("parse %q: %w", optionORTAPIVersion, err)
	}

	intraOpThreads, err := parsePositiveInt(opts[optionIntraOpThreads], defaultIntraOpThreads)
	if err != nil {
		return Config{}, fmt.Errorf("parse %q: %w", optionIntraOpThreads, err)
	}

	inputName := opts[optionInputName]
	if inputName == "" {
		inputName = inputPastValues
	}
	meanOutputName := opts[optionMeanOutputName]
	if meanOutputName == "" {
		meanOutputName = outputMeanPredictions
	}
	fullOutputName := opts[optionFullOutputName]
	if fullOutputName == "" {
		fullOutputName = outputFullPredictions
	}

	batchSize, err := parsePositiveInt(opts[optionBatchSize], defaultBatchSize)
	if err != nil {
		return Config{}, fmt.Errorf("parse %q: %w", optionBatchSize, err)
	}

	numOutputPatches, err := parsePositiveInt(opts[optionNumOutputPatches], defaultChronosOutputPatches)
	if err != nil {
		return Config{}, fmt.Errorf("parse %q: %w", optionNumOutputPatches, err)
	}

	return Config{
		ModelFamily:      modelFamily,
		ModelPath:        modelPath,
		MaxContext:       maxContext,
		RuntimeLibPath:   strings.TrimSpace(opts[optionRuntimeLibPath]),
		ORTAPIVersion:    ortVersion,
		IntraOpThreads:   intraOpThreads,
		InputName:        inputName,
		MeanOutputName:   meanOutputName,
		FullOutputName:   fullOutputName,
		BatchSize:        batchSize,
		NumOutputPatches: numOutputPatches,
	}, nil
}

func parsePositiveInt(raw string, defaultValue int) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return value, nil
}
