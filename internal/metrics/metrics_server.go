package metrics

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	podNames, err := f.resolvePodNames(ctx)
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
		return FetchResult{}, fmt.Errorf("metrics-server returned no pod metrics for %d target pods", len(podNames))
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
	return out, nil
}

func (f *metricsServerFetcher) resolvePodNames(ctx context.Context) ([]string, error) {
	ref := f.spec.TargetRef
	switch ref.Kind {
	case "Pod":
		key := types.NamespacedName{Namespace: f.namespace, Name: ref.Name}
		pod := &corev1.Pod{}
		if err := f.factory.K8sClient.Get(ctx, key, pod); err != nil {
			return nil, fmt.Errorf("get pod %q: %w", key, err)
		}
		return []string{ref.Name}, nil
	case "Deployment":
		return f.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			dep := &appsv1.Deployment{}
			if err := f.factory.K8sClient.Get(ctx, key, dep); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(dep.Spec.Selector)
		})
	case "StatefulSet":
		return f.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			sts := &appsv1.StatefulSet{}
			if err := f.factory.K8sClient.Get(ctx, key, sts); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		})
	case "ReplicaSet":
		return f.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			rs := &appsv1.ReplicaSet{}
			if err := f.factory.K8sClient.Get(ctx, key, rs); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(rs.Spec.Selector)
		})
	default:
		return nil, fmt.Errorf("unsupported targetRef kind %q", ref.Kind)
	}
}

type selectorResolver func(ctx context.Context, key types.NamespacedName) (labels.Selector, error)

func (f *metricsServerFetcher) podNamesForSelector(ctx context.Context, resolve selectorResolver) ([]string, error) {
	key := types.NamespacedName{Namespace: f.namespace, Name: f.spec.TargetRef.Name}
	selector, err := resolve(ctx, key)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get target %s %q: %w", f.spec.TargetRef.Kind, key, err)
		}
		return nil, fmt.Errorf("resolve selector for %s %q: %w", f.spec.TargetRef.Kind, key, err)
	}

	podList := &corev1.PodList{}
	if err := f.factory.K8sClient.List(ctx, podList, client.InNamespace(f.namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("list pods for %s %q: %w", f.spec.TargetRef.Kind, key, err)
	}

	names := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		names = append(names, pod.Name)
	}
	return names, nil
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
