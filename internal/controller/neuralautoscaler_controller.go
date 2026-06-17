/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/metrics"
	"github.com/pluralsh/neural-autoscaler/internal/resize"
)

const (
	conditionTypeMetricsReady = "MetricsReady"
	defaultForecastHorizon    = 12
	defaultForecastStep       = time.Minute
	defaultRequeueInterval    = 20 * time.Second
)

// NeuralAutoscalerReconciler reconciles a NeuralAutoscaler object.
type NeuralAutoscalerReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Forecaster     forecast.Forecaster
	MetricsFactory *metrics.Factory
}

//+kubebuilder:rbac:groups=autoscaling.plural.sh,resources=neuralautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.plural.sh,resources=neuralautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.plural.sh,resources=neuralautoscalers/finalizers,verbs=update
//+kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;update
//+kubebuilder:rbac:groups="",resources=pods/resize,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;replicasets,verbs=get;list
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile drives the predict→resize loop on each reconcile tick:
//
//  1. Fetch current usage from metrics-server for each configured resource and append
//     the latest sample to an in-memory per-resource history buffer.
//  2. When the buffer holds at least MinForecastSamples points, run the configured
//     ONNX forecaster over that history to produce a future usage series.
//  3. If spec.resize is set and forecasts are ready, derive per-pod container requests
//     from forecast peaks (max over horizon and quantiles, headroom factor, divided
//     by matching pod count) and clamp to spec.resize.resources min/max.
//  4. Apply the new requests in place via the pods/resize subresource. Only requests
//     are predicted; limits are raised only when they would fall below the new request.
func (r *NeuralAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	na := &v1alpha1.NeuralAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, na); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !na.DeletionTimestamp.IsZero() {
		r.evictHistory(na.Namespace, na.Name)
		return ctrl.Result{}, nil
	}

	scope, err := NewDefaultScope(ctx, r.Client, na)
	if err != nil {
		logger.Error(err, "failed to create reconciliation scope")
		return ctrl.Result{}, err
	}
	defer func() {
		if err := scope.PatchObject(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if na.Spec.Metrics.Type != v1alpha1.MetricsSourceMetricsServer {
		logger.Info("unsupported metrics source", "type", na.Spec.Metrics.Type)
		setMetricsReadyCondition(na, metav1.ConditionFalse, "UnsupportedMetricsSource", "only MetricsServer is implemented")
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
	}

	if r.MetricsFactory == nil {
		logger.Info("metrics factory not configured; skipping metrics fetch")
		setMetricsReadyCondition(na, metav1.ConditionFalse, "MetricsFactoryMissing", "metrics factory is not configured")
		return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
	}

	fetcher, err := r.MetricsFactory.NewFetcher(na.Spec.Metrics, na.Namespace)
	if err != nil {
		logger.Error(err, "invalid metrics source configuration")
		setMetricsReadyCondition(na, metav1.ConditionFalse, "InvalidMetricsSource", err.Error())
		return ctrl.Result{}, err
	}

	fetchResult, err := fetcher.Fetch(ctx)
	if err != nil {
		logger.Error(err, "failed to fetch metrics", "source", na.Spec.Metrics.Type)
		setMetricsReadyCondition(na, metav1.ConditionFalse, "MetricsFetchFailed", err.Error())
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	na.Status.LastFetchTime = &now
	na.Status.LastResourceValues = make(map[string]string, len(fetchResult.ByResource))
	maxSamples := int32(0)
	for resource, series := range fetchResult.ByResource {
		if len(series.Values) > 0 {
			last := series.Values[len(series.Values)-1]
			na.Status.LastResourceValues[string(resource)] = strconv.FormatFloat(last, 'f', -1, 64)
		}
	}

	forecasts := make(map[v1alpha1.ResourceMetric]forecast.Response)
	for _, resource := range na.Spec.Metrics.MetricsServer.Resources {
		series := fetchResult.ByResource[resource]
		key := metrics.HistoryKey(na.Namespace, na.Name, resource)
		forecastSeries := r.accumulateMetricsServerHistory(key, series)
		if count := int32(len(forecastSeries.Values)); count > maxSamples {
			maxSamples = count
		}
		if r.Forecaster != nil && na.Spec.Forecast != nil {
			if len(forecastSeries.Values) < metrics.MinForecastSamples {
				logger.Info("insufficient history for forecast",
					"resource", resource,
					"samples", len(forecastSeries.Values),
					"minimum", metrics.MinForecastSamples)
				continue
			}
			resp, err := r.runForecast(ctx, na, resource, forecastSeries)
			if err != nil {
				logger.Error(err, "forecast failed", "resource", resource)
			} else {
				forecasts[resource] = resp
			}
		}
	}
	na.Status.LastMetricsCount = maxSamples

	setMetricsReadyCondition(na, metav1.ConditionTrue, "MetricsFetched", fmt.Sprintf("fetched metrics for %d resource(s)", len(fetchResult.ByResource)))
	logger.Info("fetched metrics", "source", na.Spec.Metrics.Type, "resources", len(fetchResult.ByResource), "maxSamples", na.Status.LastMetricsCount)

	if na.Spec.Resize != nil {
		if len(forecasts) == 0 {
			logger.Info("skipping resize: forecast not ready")
		} else {
			resizeReconciler := resize.Reconciler{Client: r.Client}
			if err := resizeReconciler.Apply(ctx, na, forecasts); err != nil {
				logger.Error(err, "resize failed")
			}
		}
	}

	return ctrl.Result{RequeueAfter: defaultRequeueInterval}, nil
}

func (r *NeuralAutoscalerReconciler) accumulateMetricsServerHistory(key string, snapshot metrics.Series) metrics.Series {
	if r.MetricsFactory == nil || r.MetricsFactory.History == nil || len(snapshot.Values) == 0 {
		return snapshot
	}
	ts := snapshot.Timestamps[len(snapshot.Timestamps)-1]
	r.MetricsFactory.History.Append(key, snapshot.Values[len(snapshot.Values)-1], ts)
	return r.MetricsFactory.History.Get(key)
}

func (r *NeuralAutoscalerReconciler) evictHistory(namespace, name string) {
	if r.MetricsFactory == nil || r.MetricsFactory.History == nil {
		return
	}
	r.MetricsFactory.History.DeleteByPrefix(metrics.HistoryPrefix(namespace, name))
}

func (r *NeuralAutoscalerReconciler) runForecast(ctx context.Context, na *v1alpha1.NeuralAutoscaler, resource v1alpha1.ResourceMetric, series metrics.Series) (forecast.Response, error) {
	if err := v1alpha1.ValidateForecast(na.Spec.Forecast); err != nil {
		return forecast.Response{}, err
	}

	horizon := defaultForecastHorizon
	if na.Spec.Forecast.Horizon != nil && *na.Spec.Forecast.Horizon > 0 {
		horizon = int(*na.Spec.Forecast.Horizon)
	}

	step := defaultForecastStep
	parsedStep, err := v1alpha1.SetDuration(na.Spec.Forecast.Step)
	if err != nil {
		return forecast.Response{}, fmt.Errorf("forecast step: %w", err)
	}
	if parsedStep != nil && *parsedStep > 0 {
		step = *parsedStep
	}

	req := forecast.Request{
		SeriesID:   metrics.HistoryKey(na.Namespace, na.Name, resource),
		Values:     series.Values,
		Timestamps: series.Timestamps,
		Horizon:    horizon,
		Step:       step,
	}

	resp, err := r.Forecaster.Forecast(ctx, req)
	if err != nil {
		return forecast.Response{}, err
	}

	na.Status.LastForecast = &v1alpha1.ForecastStatus{
		Horizon:      int32(len(resp.Point)),
		ModelVersion: resp.ModelVersion,
	}

	unit := valueUnitForResource(resource)
	target := describeMetricTarget(na.Spec.Metrics, resource)

	logger := log.FromContext(ctx)
	logArgs := []any{
		"neuralAutoscaler", client.ObjectKeyFromObject(na),
		"resource", resource,
		"target", target,
		"modelVersion", resp.ModelVersion,
		"historySamples", len(series.Values),
		"step", step.String(),
		"forecast", forecast.FormatPoints(step, resp.Point, unit),
	}
	if len(resp.Quantiles) > 0 {
		logArgs = append(logArgs, "quantiles", forecast.FormatQuantiles(step, resp.Quantiles, unit))
	}
	logger.Info("forecast completed", logArgs...)

	return resp, nil
}

func describeMetricTarget(spec v1alpha1.MetricsSourceSpec, resource v1alpha1.ResourceMetric) string {
	if spec.MetricsServer == nil {
		return "metrics-server"
	}
	ms := spec.MetricsServer
	metric := string(resource)
	switch resource {
	case v1alpha1.ResourceMetricCPU:
		metric = "CPU"
	case v1alpha1.ResourceMetricMemory:
		metric = "memory"
	}
	return fmt.Sprintf("%s for %s %s", metric, ms.TargetRef.Kind, ms.TargetRef.Name)
}

func valueUnitForResource(resource v1alpha1.ResourceMetric) forecast.ValueUnit {
	switch resource {
	case v1alpha1.ResourceMetricCPU:
		return forecast.UnitMillicores
	case v1alpha1.ResourceMetricMemory:
		return forecast.UnitBytes
	default:
		return forecast.UnitGeneric
	}
}

func setMetricsReadyCondition(na *v1alpha1.NeuralAutoscaler, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionTypeMetricsReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: na.Generation,
	}
	meta.SetStatusCondition(&na.Status.Conditions, condition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NeuralAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		For(&v1alpha1.NeuralAutoscaler{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
