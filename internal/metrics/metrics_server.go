package metrics

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"github.com/pluralsh/neural-autoscaler/internal/log"
)

type metricsServerFetcher struct {
	factory   *Factory
	spec      autoscalingv1alpha1.MetricsServerSourceSpec
	namespace string
}

func newMetricsServerFetcher(factory *Factory, spec autoscalingv1alpha1.MetricsServerSourceSpec, crNamespace string) Fetcher {
	namespace := crNamespace
	if spec.Namespace != "" {
		namespace = spec.Namespace
	}
	return &metricsServerFetcher{
		factory:   factory,
		spec:      spec,
		namespace: namespace,
	}
}

func (f *metricsServerFetcher) Fetch(ctx context.Context) (FetchResult, error) {
	podNames, err := ResolvePodNames(ctx, f.factory.K8sClient, f.namespace, f.spec.TargetRef)
	if err != nil {
		return FetchResult{}, err
	}
	if len(podNames) == 0 {
		return FetchResult{}, fmt.Errorf("no pods found for %s/%s", f.spec.TargetRef.Kind, f.spec.TargetRef.Name)
	}

	list, err := f.factory.MetricsClient.PodMetricses(f.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return FetchResult{}, fmt.Errorf("list pod metrics in namespace %q: %w", f.namespace, err)
	}

	podNameSet := make(map[string]struct{}, len(podNames))
	for _, name := range podNames {
		podNameSet[name] = struct{}{}
	}

	totals := make(map[autoscalingv1alpha1.ResourceMetric]float64, len(f.spec.Resources))
	for _, metric := range f.spec.Resources {
		totals[metric] = 0
	}

	var matched int
	for _, item := range list.GetItems() {
		if _, ok := podNameSet[item.GetName()]; !ok {
			continue
		}
		for _, metric := range f.spec.Resources {
			switch metric {
			case autoscalingv1alpha1.ResourceMetricCPU:
				totals[metric] += float64(item.CPUMillicores())
			case autoscalingv1alpha1.ResourceMetricMemory:
				totals[metric] += float64(item.MemoryBytes())
			default:
				return FetchResult{}, fmt.Errorf("unsupported metric %q", metric)
			}
		}
		matched++
	}
	if matched == 0 {
		err := fmt.Errorf("metrics-server returned no pod metrics for %d target pods", len(podNames))
		log.Error(err, "metrics-server fetch failed", "namespace", f.namespace, "targetPods", len(podNames))
		return FetchResult{}, err
	}

	now := f.factory.now()
	out := FetchResult{
		ByResource: make(map[autoscalingv1alpha1.ResourceMetric]Series, len(totals)),
		PodNames:   append([]string(nil), podNames...),
	}
	for metric, total := range totals {
		out.ByResource[metric] = Series{
			Values:     []float64{total},
			Timestamps: []time.Time{now},
		}
	}
	log.Debug("fetched metrics-server pod metrics", "namespace", f.namespace, "targetPods", len(podNames), "matchedPods", matched)
	return out, nil
}

// HistoryKey returns the per-resource history buffer key for a NeuralAutoscaler.
func HistoryKey(namespace, name string, resource autoscalingv1alpha1.ResourceMetric) string {
	return fmt.Sprintf("%s/%s/%s", namespace, name, resource)
}

// HistoryPrefix returns the prefix for all history keys belonging to a NeuralAutoscaler.
func HistoryPrefix(namespace, name string) string {
	return fmt.Sprintf("%s/%s/", namespace, name)
}

// PodMetricsNamespaceClient is the namespace-scoped pod metrics API.
type PodMetricsNamespaceClient interface {
	List(ctx context.Context, opts metav1.ListOptions) (*metricsv1beta1.PodMetricsList, error)
}

// NewMetricsClientAdapter wraps a metrics.k8s.io clientset for use by the factory.
func NewMetricsClientAdapter(client metricsclientset.Interface) PodMetricsClient {
	return metricsClientAdapter{client: client}
}

type metricsClientAdapter struct {
	client metricsclientset.Interface
}

func (a metricsClientAdapter) PodMetricses(namespace string) PodMetricsNamespace {
	return metricsNamespaceAdapter{client: a.client.MetricsV1beta1().PodMetricses(namespace)}
}

type metricsNamespaceAdapter struct {
	client PodMetricsNamespaceClient
}

func (a metricsNamespaceAdapter) List(ctx context.Context, opts metav1.ListOptions) (PodMetricsList, error) {
	list, err := a.client.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	return podMetricsListAdapter{list: list}, nil
}

type podMetricsListAdapter struct {
	list *metricsv1beta1.PodMetricsList
}

func (a podMetricsListAdapter) GetItems() []PodMetric {
	items := make([]PodMetric, 0, len(a.list.Items))
	for i := range a.list.Items {
		items = append(items, podMetricAdapter{pod: &a.list.Items[i]})
	}
	return items
}

type podMetricAdapter struct {
	pod *metricsv1beta1.PodMetrics
}

func (a podMetricAdapter) GetName() string {
	return a.pod.Name
}

func (a podMetricAdapter) CPUMillicores() int64 {
	var total int64
	for _, container := range a.pod.Containers {
		if q, ok := container.Usage[corev1.ResourceCPU]; ok {
			total += q.MilliValue()
		}
	}
	return total
}

func (a podMetricAdapter) MemoryBytes() int64 {
	var total int64
	for _, container := range a.pod.Containers {
		if q, ok := container.Usage[corev1.ResourceMemory]; ok {
			total += q.Value()
		}
	}
	return total
}
