package onnx

import (
	"fmt"
	"strings"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
)

// DiscoverModelFamily opens the ONNX model briefly and infers timesfm vs chronos2
// from session input tensor names, with an optional path-based fallback.
func DiscoverModelFamily(modelPath, runtimeLibPath string, ortAPIVersion int) (string, error) {
	rt, err := ort.NewRuntime(runtimeLibPath, uint32(ortAPIVersion))
	if err != nil {
		return "", fmt.Errorf("create runtime: %w", err)
	}
	defer rt.Close()

	env, err := rt.NewEnv("neural-autoscaler-discover", ort.LoggingLevelWarning)
	if err != nil {
		return "", fmt.Errorf("create env: %w", err)
	}
	defer env.Close()

	session, err := rt.NewSession(env, modelPath, &ort.SessionOptions{
		IntraOpNumThreads: 1,
	})
	if err != nil {
		return "", fmt.Errorf("open session: %w", err)
	}
	defer session.Close()

	inputNames := session.InputNames()
	if family, ok := familyFromInputNames(inputNames); ok {
		return family, nil
	}

	if family, ok := familyFromModelPath(modelPath); ok {
		return family, nil
	}

	return "", fmt.Errorf("unable to infer family from input names %v", inputNames)
}

func familyFromInputNames(names []string) (string, bool) {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}

	if _, ok := set[inputPastValues]; ok {
		return ModelFamilyTimesFM, true
	}

	if _, ok := set[inputContext]; ok {
		return ModelFamilyChronos2, true
	}

	return "", false
}

func familyFromModelPath(modelPath string) (string, bool) {
	lower := strings.ToLower(modelPath)
	if strings.Contains(lower, "chronos") {
		return ModelFamilyChronos2, true
	}
	if strings.Contains(lower, "timesfm") {
		return ModelFamilyTimesFM, true
	}
	return "", false
}
