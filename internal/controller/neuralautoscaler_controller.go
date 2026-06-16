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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/forecast"
	"github.com/pluralsh/neural-autoscaler/internal/metrics"
)

const (
	conditionTypeMetricsReady = "MetricsReady"
	defaultForecastHorizon    = 12
	defaultForecastStep       = time.Minute
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
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;replicasets,verbs=get;list
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get

func (r *NeuralAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	na := &autoscalingv1alpha1.NeuralAutoscaler{}
	if err := r.Get(ctx, req.NamespacedName, na); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	seriesKey := fmt.Sprintf("%s/%s", na.Namespace, na.Name)
	if !na.DeletionTimestamp.IsZero() {
		r.evictHistory(seriesKey)
	}

	if r.MetricsFactory == nil {
		logger.Info("metrics factory not configured; skipping metrics fetch")
		setMetricsReadyCondition(na, metav1.ConditionFalse, "MetricsFactoryMissing", "metrics factory is not configured")
		return r.updateStatus(ctx, na)
	}

	fetcher, err := r.MetricsFactory.NewFetcher(na.Spec.Metrics, na.Namespace)
	if err != nil {
		logger.Error(err, "invalid metrics source configuration")
		setMetricsReadyCondition(na, metav1.ConditionFalse, "InvalidMetricsSource", err.Error())
		return r.updateStatus(ctx, na)
	}

	series, err := fetcher.Fetch(ctx)
	if err != nil {
		logger.Error(err, "failed to fetch metrics", "source", na.Spec.Metrics.Type)
		setMetricsReadyCondition(na, metav1.ConditionFalse, "MetricsFetchFailed", err.Error())
		return r.updateStatus(ctx, na)
	}

	now := metav1.Now()
	na.Status.LastFetchTime = &now
	if len(series.Values) > 0 {
		last := series.Values[len(series.Values)-1]
		na.Status.LastMetricValue = strconv.FormatFloat(last, 'f', -1, 64)
	}

	forecastSeries := series
	if na.Spec.Metrics.Type == autoscalingv1alpha1.MetricsSourceMetricsServer {
		forecastSeries = r.accumulateMetricsServerHistory(seriesKey, series)
		na.Status.LastMetricsCount = int32(len(forecastSeries.Values))
	} else {
		na.Status.LastMetricsCount = int32(len(series.Values))
	}

	setMetricsReadyCondition(na, metav1.ConditionTrue, "MetricsFetched", fmt.Sprintf("fetched %d metric samples", na.Status.LastMetricsCount))
	logger.Info("fetched metrics", "source", na.Spec.Metrics.Type, "samples", na.Status.LastMetricsCount, "lastValue", na.Status.LastMetricValue)

	if r.Forecaster != nil && na.Spec.Forecast != nil {
		if len(forecastSeries.Values) < metrics.MinForecastSamples {
			logger.Info("insufficient history for forecast",
				"samples", len(forecastSeries.Values),
				"minimum", metrics.MinForecastSamples)
		} else if err := r.runForecast(ctx, na, forecastSeries); err != nil {
			logger.Error(err, "forecast failed")
		}
	}

	return r.updateStatus(ctx, na)
}

func (r *NeuralAutoscalerReconciler) accumulateMetricsServerHistory(key string, snapshot metrics.Series) metrics.Series {
	if r.MetricsFactory == nil || r.MetricsFactory.History == nil || len(snapshot.Values) == 0 {
		return snapshot
	}
	ts := snapshot.Timestamps[len(snapshot.Timestamps)-1]
	r.MetricsFactory.History.Append(key, snapshot.Values[len(snapshot.Values)-1], ts)
	return r.MetricsFactory.History.Get(key)
}

func (r *NeuralAutoscalerReconciler) evictHistory(key string) {
	if r.MetricsFactory == nil || r.MetricsFactory.History == nil {
		return
	}
	r.MetricsFactory.History.Delete(key)
}

func (r *NeuralAutoscalerReconciler) runForecast(ctx context.Context, na *autoscalingv1alpha1.NeuralAutoscaler, series metrics.Series) error {
	if err := autoscalingv1alpha1.ValidateForecast(na.Spec.Forecast); err != nil {
		return err
	}

	horizon := defaultForecastHorizon
	if na.Spec.Forecast.Horizon != nil && *na.Spec.Forecast.Horizon > 0 {
		horizon = int(*na.Spec.Forecast.Horizon)
	}

	step := defaultForecastStep
	parsedStep, err := autoscalingv1alpha1.SetDuration(na.Spec.Forecast.Step)
	if err != nil {
		return fmt.Errorf("forecast step: %w", err)
	}
	if parsedStep != nil && *parsedStep > 0 {
		step = *parsedStep
	}

	req := forecast.Request{
		SeriesID:   fmt.Sprintf("%s/%s", na.Namespace, na.Name),
		Values:     series.Values,
		Timestamps: series.Timestamps,
		Horizon:    horizon,
		Step:       step,
	}

	resp, err := r.Forecaster.Forecast(ctx, req)
	if err != nil {
		return err
	}

	na.Status.LastForecast = &autoscalingv1alpha1.ForecastStatus{
		Horizon:      int32(len(resp.Point)),
		ModelVersion: resp.ModelVersion,
	}

	unit := valueUnitFromSpec(na.Spec.Metrics)
	target := describeMetricTarget(na.Spec.Metrics)

	logger := log.FromContext(ctx)
	logArgs := []any{
		"neuralAutoscaler", client.ObjectKeyFromObject(na),
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

	return nil
}

func describeMetricTarget(spec autoscalingv1alpha1.MetricsSourceSpec) string {
	switch spec.Type {
	case autoscalingv1alpha1.MetricsSourceMetricsServer:
		if spec.MetricsServer == nil {
			return "metrics-server"
		}
		ms := spec.MetricsServer
		metric := string(ms.Metric)
		switch ms.Metric {
		case autoscalingv1alpha1.ResourceMetricCPU:
			metric = "CPU"
		case autoscalingv1alpha1.ResourceMetricMemory:
			metric = "memory"
		}
		return fmt.Sprintf("%s for %s %s", metric, ms.TargetRef.Kind, ms.TargetRef.Name)
	case autoscalingv1alpha1.MetricsSourcePrometheus:
		return "Prometheus query"
	default:
		return string(spec.Type)
	}
}

func valueUnitFromSpec(spec autoscalingv1alpha1.MetricsSourceSpec) forecast.ValueUnit {
	if spec.Type == autoscalingv1alpha1.MetricsSourceMetricsServer && spec.MetricsServer != nil {
		switch spec.MetricsServer.Metric {
		case autoscalingv1alpha1.ResourceMetricCPU:
			return forecast.UnitMillicores
		case autoscalingv1alpha1.ResourceMetricMemory:
			return forecast.UnitBytes
		}
	}
	return forecast.UnitGeneric
}

func setMetricsReadyCondition(na *autoscalingv1alpha1.NeuralAutoscaler, status metav1.ConditionStatus, reason, message string) {
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

func (r *NeuralAutoscalerReconciler) updateStatus(ctx context.Context, na *autoscalingv1alpha1.NeuralAutoscaler) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, na); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 20 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NeuralAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.NeuralAutoscaler{}).
		Complete(r)
}
