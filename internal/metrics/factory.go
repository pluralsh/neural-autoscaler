package metrics

import (
	"context"
	"fmt"
	"time"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PodMetricsClient lists pod metrics from the metrics.k8s.io API.
type PodMetricsClient interface {
	PodMetricses(namespace string) PodMetricsNamespace
}

// PodMetricsNamespace lists pod metrics in a namespace.
type PodMetricsNamespace interface {
	List(ctx context.Context, opts metav1.ListOptions) (PodMetricsList, error)
}

// PodMetricsList contains pod metric entries.
type PodMetricsList interface {
	GetItems() []PodMetric
}

// PodMetric is a single pod metrics record.
type PodMetric interface {
	GetName() string
	CPUMillicores() int64
	MemoryBytes() int64
}

// Factory builds Fetcher implementations from CRD specs.
type Factory struct {
	K8sClient     client.Client
	MetricsClient PodMetricsClient
	History       *HistoryStore
	HTTPClient    HTTPDoer
	Now           func() time.Time
}

// NewFetcher returns a Fetcher for the given metrics source spec.
func (f *Factory) NewFetcher(spec autoscalingv1alpha1.MetricsSourceSpec, namespace string) (Fetcher, error) {
	if err := autoscalingv1alpha1.ValidateMetricsSource(spec); err != nil {
		return nil, err
	}
	if f == nil {
		return nil, fmt.Errorf("metrics factory is nil")
	}

	switch spec.Type {
	case autoscalingv1alpha1.MetricsSourceMetricsServer:
		if f.K8sClient == nil || f.MetricsClient == nil {
			return nil, fmt.Errorf("kubernetes and metrics clients are required for MetricsServer")
		}
		return newMetricsServerFetcher(f, *spec.MetricsServer, namespace), nil
	case autoscalingv1alpha1.MetricsSourcePrometheus:
		if spec.Prometheus.TargetRef != nil && f.K8sClient == nil {
			return nil, fmt.Errorf("kubernetes client is required for Prometheus targetRef resolution")
		}
		if spec.Prometheus.Auth != nil && f.K8sClient == nil {
			return nil, fmt.Errorf("kubernetes client is required for Prometheus auth")
		}
		return newPrometheusFetcher(f, *spec.Prometheus, namespace), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedSource, spec.Type)
	}
}

func (f *Factory) now() time.Time {
	if f != nil && f.Now != nil {
		return f.Now()
	}
	return time.Now()
}
