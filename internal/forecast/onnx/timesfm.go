package onnx

import (
	"context"
	"fmt"
	"sync"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

// TimesFM loads and runs the TimesFM 2.5 ONNX export.
type TimesFM struct {
	cfg        Config
	runtime    *ort.Runtime
	env        *ort.Env
	session    *ort.Session
	inputName  string
	meanOutput string
	fullOutput string
	batchSize  int
	mu         sync.Mutex
}

func newTimesFM(onnxCfg Config) (forecast.Forecaster, error) {
	rt, err := ort.NewRuntime(onnxCfg.RuntimeLibPath, uint32(onnxCfg.ORTAPIVersion))
	if err != nil {
		return nil, fmt.Errorf("load timesfm onnx model: create runtime: %w", err)
	}

	env, err := rt.NewEnv("neural-autoscaler", ort.LoggingLevelWarning)
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("load timesfm onnx model: create env: %w", err)
	}

	session, err := rt.NewSession(env, onnxCfg.ModelPath, &ort.SessionOptions{
		IntraOpNumThreads: onnxCfg.IntraOpThreads,
	})
	if err != nil {
		env.Close()
		rt.Close()
		return nil, fmt.Errorf("load timesfm onnx model: %w", err)
	}

	inputName := onnxCfg.InputName
	if names := session.InputNames(); len(names) > 0 {
		inputName = names[0]
	}

	meanOutput := onnxCfg.MeanOutputName
	fullOutput := onnxCfg.FullOutputName
	for _, name := range session.OutputNames() {
		switch name {
		case outputMeanPredictions:
			meanOutput = name
		case outputFullPredictions:
			fullOutput = name
		}
	}

	return &TimesFM{
		cfg:        onnxCfg,
		runtime:    rt,
		env:        env,
		session:    session,
		inputName:  inputName,
		meanOutput: meanOutput,
		fullOutput: fullOutput,
		batchSize:  onnxCfg.BatchSize,
	}, nil
}

func (t *TimesFM) Ready(_ context.Context) error {
	if t == nil || t.session == nil {
		return forecast.ErrBackendNotReady
	}
	return nil
}

func (t *TimesFM) Forecast(ctx context.Context, req forecast.Request) (forecast.Response, error) {
	if err := forecast.ValidateRequest(req); err != nil {
		return forecast.Response{}, err
	}
	if err := t.Ready(ctx); err != nil {
		return forecast.Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return forecast.Response{}, err
	}

	pastValues, contextLen := prepareBatchInput(req.Values, t.cfg.MaxContext, t.batchSize)
	input, err := ort.NewTensorValue(t.runtime, pastValues, []int64{int64(t.batchSize), int64(contextLen)})
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build input tensor: %w", err)
	}
	defer input.Close()

	t.mu.Lock()
	outputs, err := t.session.Run(ctx, map[string]*ort.Value{
		t.inputName: input,
	})
	t.mu.Unlock()
	if err != nil {
		return forecast.Response{}, fmt.Errorf("run timesfm onnx model: %w", err)
	}
	defer closeValues(outputs)

	meanTensor, ok := outputs[t.meanOutput]
	if !ok {
		return forecast.Response{}, fmt.Errorf("missing output %q", t.meanOutput)
	}

	meanData, meanShape, err := ort.GetTensorData[float32](meanTensor)
	if err != nil {
		return forecast.Response{}, fmt.Errorf("read mean predictions: %w", err)
	}

	resp := forecast.Response{
		Point:        sliceHorizon(meanData, req.Horizon),
		ModelVersion: modelVersion,
	}

	if len(req.Quantiles) == 0 {
		return resp, nil
	}

	fullTensor, ok := outputs[t.fullOutput]
	if !ok {
		return resp, nil
	}

	fullData, fullShape, err := ort.GetTensorData[float32](fullTensor)
	if err != nil {
		return forecast.Response{}, fmt.Errorf("read quantile predictions: %w", err)
	}
	_ = meanShape

	for _, level := range req.Quantiles {
		resp.Quantiles = append(resp.Quantiles, forecast.QuantileSeries{
			Level:  level,
			Values: quantileSeries(fullData, fullShape, level, req.Horizon),
		})
	}

	return resp, nil
}

func (t *TimesFM) Close() error {
	if t == nil {
		return nil
	}
	if t.session != nil {
		t.session.Close()
		t.session = nil
	}
	if t.env != nil {
		t.env.Close()
		t.env = nil
	}
	if t.runtime != nil {
		t.runtime.Close()
		t.runtime = nil
	}
	return nil
}

func closeValues(values map[string]*ort.Value) {
	for _, value := range values {
		if value != nil {
			value.Close()
		}
	}
}
