package onnx

import (
	"context"
	"fmt"
	"sync"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"

	"github.com/pluralsh/neural-autoscaler/internal/forecast"
)

const (
	inputContext          = "context"
	inputGroupIDs         = "group_ids"
	inputAttentionMask    = "attention_mask"
	inputFutureCovariates = "future_covariates"
	inputNumOutputPatches = "num_output_patches"
	outputQuantilePreds   = "quantile_preds"
)

// Chronos2 loads and runs the Chronos-2 ONNX export (TSFM-ai/chronos-2-onnx).
type Chronos2 struct {
	cfg              Config
	runtime          *ort.Runtime
	env              *ort.Env
	session          *ort.Session
	numOutputPatches int64
	mu               sync.Mutex
}

func newChronos2(onnxCfg Config) (forecast.Forecaster, error) {
	rt, err := ort.NewRuntime(onnxCfg.RuntimeLibPath, uint32(onnxCfg.ORTAPIVersion))
	if err != nil {
		return nil, fmt.Errorf("load chronos-2 onnx model: create runtime: %w", err)
	}

	env, err := rt.NewEnv("neural-autoscaler", ort.LoggingLevelWarning)
	if err != nil {
		rt.Close()
		return nil, fmt.Errorf("load chronos-2 onnx model: create env: %w", err)
	}

	session, err := rt.NewSession(env, onnxCfg.ModelPath, &ort.SessionOptions{
		IntraOpNumThreads: onnxCfg.IntraOpThreads,
	})
	if err != nil {
		env.Close()
		rt.Close()
		return nil, fmt.Errorf("load chronos-2 onnx model: %w", err)
	}

	return &Chronos2{
		cfg:              onnxCfg,
		runtime:          rt,
		env:              env,
		session:          session,
		numOutputPatches: int64(onnxCfg.NumOutputPatches),
	}, nil
}

func (c *Chronos2) Ready(_ context.Context) error {
	if c == nil || c.session == nil {
		return forecast.ErrBackendNotReady
	}
	return nil
}

func (c *Chronos2) Forecast(ctx context.Context, req forecast.Request) (forecast.Response, error) {
	if err := forecast.ValidateRequest(req); err != nil {
		return forecast.Response{}, err
	}
	if err := c.Ready(ctx); err != nil {
		return forecast.Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return forecast.Response{}, err
	}

	prepared := prepareChronosInputs(req.Values)

	contextTensor, err := ort.NewTensorValue(c.runtime, prepared.context, []int64{1, chronosContextLen})
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build context tensor: %w", err)
	}
	defer contextTensor.Close()

	groupIDsTensor, err := ort.NewTensorValue(c.runtime, []int64{0}, []int64{1})
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build group_ids tensor: %w", err)
	}
	defer groupIDsTensor.Close()

	maskTensor, err := ort.NewTensorValue(c.runtime, prepared.attentionMask, []int64{1, chronosContextLen})
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build attention_mask tensor: %w", err)
	}
	defer maskTensor.Close()

	futureCovTensor, err := ort.NewTensorValue(c.runtime, prepared.futureCovariates, []int64{1, chronosFutureCovLen})
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build future_covariates tensor: %w", err)
	}
	defer futureCovTensor.Close()

	numPatchesTensor, err := ort.NewTensorValue(c.runtime, []int64{c.numOutputPatches}, nil)
	if err != nil {
		return forecast.Response{}, fmt.Errorf("build num_output_patches tensor: %w", err)
	}
	defer numPatchesTensor.Close()

	inputs := map[string]*ort.Value{
		inputContext:          contextTensor,
		inputGroupIDs:         groupIDsTensor,
		inputAttentionMask:    maskTensor,
		inputFutureCovariates: futureCovTensor,
		inputNumOutputPatches: numPatchesTensor,
	}

	c.mu.Lock()
	outputs, err := c.session.Run(ctx, inputs)
	c.mu.Unlock()
	if err != nil {
		return forecast.Response{}, fmt.Errorf("run chronos-2 onnx model: %w", err)
	}
	defer closeValues(outputs)

	quantileTensor, ok := outputs[outputQuantilePreds]
	if !ok {
		return forecast.Response{}, fmt.Errorf("missing output %q", outputQuantilePreds)
	}

	quantileData, quantileShape, err := ort.GetTensorData[float32](quantileTensor)
	if err != nil {
		return forecast.Response{}, fmt.Errorf("read quantile predictions: %w", err)
	}
	if len(quantileShape) != 3 || int(quantileShape[1]) != chronosQuantileCount {
		return forecast.Response{}, fmt.Errorf("unexpected quantile_preds shape %v", quantileShape)
	}

	resp := forecast.Response{
		Point:        chronosPointSeries(quantileData, prepared.scale, req.Horizon),
		ModelVersion: chronosModelVersion,
	}

	for _, level := range req.Quantiles {
		idx := chronosQuantileIndex(level)
		resp.Quantiles = append(resp.Quantiles, forecast.QuantileSeries{
			Level:  level,
			Values: chronosQuantileHorizon(quantileData, idx, prepared.scale, req.Horizon),
		})
	}

	return resp, nil
}

func (c *Chronos2) Close() error {
	if c == nil {
		return nil
	}
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	if c.env != nil {
		c.env.Close()
		c.env = nil
	}
	if c.runtime != nil {
		c.runtime.Close()
		c.runtime = nil
	}
	return nil
}
