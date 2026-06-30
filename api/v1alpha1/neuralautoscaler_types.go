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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricsSourceType selects where historical workload metrics are fetched from.
// +kubebuilder:validation:Enum=MetricsServer;Prometheus
type MetricsSourceType string

const (
	MetricsSourceMetricsServer MetricsSourceType = "MetricsServer"
	MetricsSourcePrometheus    MetricsSourceType = "Prometheus"
)

// ResourceMetric is a well-known container resource metric exposed by metrics-server.
// +kubebuilder:validation:Enum=cpu;memory
type ResourceMetric string

const (
	ResourceMetricCPU    ResourceMetric = "cpu"
	ResourceMetricMemory ResourceMetric = "memory"
)

// PrometheusQueryType selects the Prometheus HTTP API endpoint used for reads.
// +kubebuilder:validation:Enum=query;query_range
type PrometheusQueryType string

const (
	PrometheusQueryInstant PrometheusQueryType = "query"
	PrometheusQueryRange   PrometheusQueryType = "query_range"
)

// MetricsSourceSpec configures the metrics backend for this autoscaler.
// +kubebuilder:validation:XValidation:rule="self.type != 'MetricsServer' || has(self.metricsServer)",message="metricsServer is required when type is MetricsServer"
// +kubebuilder:validation:XValidation:rule="self.type != 'Prometheus' || has(self.prometheus)",message="prometheus is required when type is Prometheus"
type MetricsSourceSpec struct {
	// Type selects the metrics backend.
	// +kubebuilder:validation:Required
	Type MetricsSourceType `json:"type"`

	// MetricsServer fetches current pod resource usage from the Kubernetes metrics API.
	MetricsServer *MetricsServerSourceSpec `json:"metricsServer,omitempty"`

	// Prometheus executes a PromQL query against a Prometheus-compatible HTTP API.
	Prometheus *PrometheusSourceSpec `json:"prometheus,omitempty"`
}

// MetricsServerSourceSpec reads aggregate pod metrics for a target workload.
type MetricsServerSourceSpec struct {
	// TargetRef identifies the scaled workload (Deployment, StatefulSet, ReplicaSet, or Pod).
	TargetRef CrossVersionObjectReference `json:"targetRef"`

	// Resources lists container resources to aggregate across matching pods.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Resources []ResourceMetric `json:"resources"`

	// Namespace overrides the NeuralAutoscaler namespace for target resolution and pod metrics reads.
	// Defaults to the NeuralAutoscaler object namespace when unset.
	Namespace string `json:"namespace,omitempty"`
}

// CrossVersionObjectReference identifies a Kubernetes object without a fixed API version.
type CrossVersionObjectReference struct {
	// APIGroup of the referent. Defaults to "apps" for Deployments and StatefulSets.
	// +optional
	APIGroup string `json:"apiGroup,omitempty"`

	// Kind of the referent.
	// +kubebuilder:validation:Enum=Deployment;StatefulSet;ReplicaSet;Pod
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// Name of the referent.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// PrometheusSourceSpec queries Prometheus for workload resource metrics.
// When targetRef and resources are set, PromQL is built automatically from cAdvisor
// container metrics. Query is optional and selects legacy single-query mode when set
// without targetRef.
type PrometheusSourceSpec struct {
	// URL is the Prometheus server base URL, for example "http://prometheus.monitoring:9090".
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// TargetRef identifies the scaled workload (Deployment, StatefulSet, ReplicaSet, or Pod).
	// Required unless query is set for legacy single-query mode.
	// +optional
	TargetRef *CrossVersionObjectReference `json:"targetRef,omitempty"`

	// Resources lists container resources to aggregate across matching pods.
	// Required when targetRef is set.
	// +optional
	Resources []ResourceMetric `json:"resources,omitempty"`

	// Namespace overrides the NeuralAutoscaler namespace for target resolution and PromQL filters.
	// Defaults to the NeuralAutoscaler object namespace when unset.
	Namespace string `json:"namespace,omitempty"`

	// Query is an optional PromQL expression override. When unset, queries are built from
	// targetRef and resources. When set without targetRef, legacy single-query mode is used.
	// +optional
	Query string `json:"query,omitempty"`

	// QueryType selects /api/v1/query (instant) or /api/v1/query_range (range).
	// Defaults to query_range.
	// +optional
	QueryType PrometheusQueryType `json:"queryType,omitempty"`

	// Step is the range query resolution width.
	// Defaults to 1m when queryType is query_range.
	// +optional
	Step *string `json:"step,omitempty"`

	// Lookback limits how far back query_range reads historical samples.
	// Defaults to 1h when queryType is query_range.
	// +optional
	Lookback *string `json:"lookback,omitempty"`

	// Auth optionally supplies credentials from a Secret in the same namespace as the NeuralAutoscaler.
	// +optional
	Auth *corev1.SecretKeySelector `json:"auth,omitempty"`
}

// DefaultMinChangePercent is applied when resize.minChangePercent and a resource's
// minChangePercent override are both unset.
const DefaultMinChangePercent int32 = 10

// ResizeSpec configures in-place vertical scaling of pods resolved from
// metrics.metricsServer.targetRef. Each entry in resources is driven by the
// matching metric from metrics.metricsServer.resources.
type ResizeSpec struct {
	// ContainerName optionally selects which container in each target pod is resized.
	// Set to "*" to target all containers in a pod.
	// When unset, the primary container (spec.containers[0]) is used.
	// +kubebuilder:validation:MinLength=1
	// +optional
	ContainerName *string `json:"containerName,omitempty"`

	// MinChangePercent is the minimum relative change required before a resource
	// request is updated in place. Compares |new-old|/old*100; when old is zero,
	// any positive new value counts as a change. Defaults to 10 when unset.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinChangePercent *int32 `json:"minChangePercent,omitempty"`

	// Resources defines per-resource resize bounds. Keys must be cpu and/or memory.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	Resources map[string]ResourceBoundsSpec `json:"resources"`
}

// ResourceBoundsSpec defines optional min/max quantity bounds for a resize target resource.
type ResourceBoundsSpec struct {
	// Min is the minimum allowed quantity, for example "250m" or "512Mi".
	// +optional
	Min *string `json:"min,omitempty"`

	// Max is the maximum allowed quantity, for example "8" or "16Gi".
	// +optional
	Max *string `json:"max,omitempty"`

	// MinChangePercent overrides resize.minChangePercent for this resource.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinChangePercent *int32 `json:"minChangePercent,omitempty"`
}

// ForecastSpec configures optional ONNX forecasting for fetched metrics.
type ForecastSpec struct {
	// Horizon is the number of future steps to predict.
	// +kubebuilder:validation:Minimum=1
	// +optional
	Horizon *int32 `json:"horizon,omitempty"`

	// Step is the interval between forecasted points.
	// Defaults to 1m when unset.
	// +optional
	Step *string `json:"step,omitempty"`
}

// NeuralAutoscalerSpec defines the desired state of NeuralAutoscaler.
type NeuralAutoscalerSpec struct {
	// Metrics configures where workload metrics are fetched from.
	// +kubebuilder:validation:Required
	Metrics MetricsSourceSpec `json:"metrics"`

	// Forecast optionally enables ONNX forecasting when the operator is started with a model.
	// +optional
	Forecast *ForecastSpec `json:"forecast,omitempty"`

	// Resize optionally enables in-place pod resource resizing after a forecast is available.
	// +optional
	Resize *ResizeSpec `json:"resize,omitempty"`
}

// ForecastStatus records the most recent forecast summary written by the controller.
type ForecastStatus struct {
	// Horizon is the number of predicted points returned.
	Horizon int32 `json:"horizon,omitempty"`

	// ModelVersion identifies the ONNX model used for the forecast.
	ModelVersion string `json:"modelVersion,omitempty"`
}

// NeuralAutoscalerStatus defines the observed state of NeuralAutoscaler.
type NeuralAutoscalerStatus struct {
	// LastFetchTime is when metrics were last fetched successfully.
	// +optional
	LastFetchTime *metav1.Time `json:"lastFetchTime,omitempty"`

	// LastResourceValues maps resource name to the most recent fetched sample value.
	// +optional
	LastResourceValues map[string]string `json:"lastResourceValues,omitempty"`

	// LastMetricsCount is the number of samples in the longest buffered history series.
	// +optional
	LastMetricsCount int32 `json:"lastMetricsCount,omitempty"`

	// LastForecast summarizes the most recent forecast, when forecasting is enabled.
	// +optional
	LastForecast *ForecastStatus `json:"lastForecast,omitempty"`

	// Conditions represent the current state of the NeuralAutoscaler resource.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.metrics.type`
//+kubebuilder:printcolumn:name="Samples",type=integer,JSONPath=`.status.lastMetricsCount`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NeuralAutoscaler is the Schema for the neuralautoscalers API.
type NeuralAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NeuralAutoscalerSpec   `json:"spec,omitempty"`
	Status NeuralAutoscalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NeuralAutoscalerList contains a list of NeuralAutoscaler.
type NeuralAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NeuralAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NeuralAutoscaler{}, &NeuralAutoscalerList{})
}
