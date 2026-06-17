package metrics

import (
	"context"
	"testing"
	"time"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMetricsServerFetcherFetch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = autoscalingv1alpha1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "api-1",
			Labels:    map[string]string{"app": "api"},
		},
	}

	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	metricsClient := fakePodMetricsClient{
		items: []PodMetric{
			podMetricStub{name: "api-1", cpu: 250, memory: 128 * 1024 * 1024},
		},
	}

	factory := &Factory{
		K8sClient:     k8sClient,
		MetricsClient: metricsClient,
		Now:           func() time.Time { return time.Unix(1_700_000_000, 0) },
	}

	fetcher, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourceMetricsServer,
		MetricsServer: &autoscalingv1alpha1.MetricsServerSourceSpec{
			TargetRef: autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Pod", Name: "api-1"},
			Resources: []autoscalingv1alpha1.ResourceMetric{
				autoscalingv1alpha1.ResourceMetricCPU,
				autoscalingv1alpha1.ResourceMetricMemory,
			},
		},
	}, "default")
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}

	result, err := fetcher.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	cpu := result.ByResource[autoscalingv1alpha1.ResourceMetricCPU]
	if len(cpu.Values) != 1 || cpu.Values[0] != 250 {
		t.Fatalf("unexpected cpu series: %#v", cpu)
	}
	mem := result.ByResource[autoscalingv1alpha1.ResourceMetricMemory]
	if len(mem.Values) != 1 || mem.Values[0] != float64(128*1024*1024) {
		t.Fatalf("unexpected memory series: %#v", mem)
	}
	if len(result.PodNames) != 1 || result.PodNames[0] != "api-1" {
		t.Fatalf("unexpected pod names: %#v", result.PodNames)
	}
}

type fakePodMetricsClient struct {
	items []PodMetric
}

func (f fakePodMetricsClient) PodMetricses(string) PodMetricsNamespace {
	return fakePodMetricsNamespace{items: f.items}
}

type fakePodMetricsNamespace struct {
	items []PodMetric
}

func (f fakePodMetricsNamespace) List(context.Context, metav1.ListOptions) (PodMetricsList, error) {
	return fakePodMetricsList{items: f.items}, nil
}

type fakePodMetricsList struct {
	items []PodMetric
}

func (f fakePodMetricsList) GetItems() []PodMetric {
	return f.items
}

type podMetricStub struct {
	name   string
	cpu    int64
	memory int64
}

func (p podMetricStub) GetName() string      { return p.name }
func (p podMetricStub) CPUMillicores() int64 { return p.cpu }
func (p podMetricStub) MemoryBytes() int64   { return p.memory }

func TestPodMetricAdapterAggregatesContainers(t *testing.T) {
	t.Parallel()

	pod := &metricsv1beta1.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "api-1"},
		Containers: []metricsv1beta1.ContainerMetrics{
			{Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			}},
			{Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("32Mi"),
			}},
		},
	}

	adapter := podMetricAdapter{pod: pod}
	if got := adapter.CPUMillicores(); got != 150 {
		t.Fatalf("CPUMillicores() = %d, want 150", got)
	}
	if got := adapter.MemoryBytes(); got != pod.Containers[0].Usage.Memory().Value()+pod.Containers[1].Usage.Memory().Value() {
		t.Fatalf("MemoryBytes() = %d", got)
	}
}

func TestHistoryKey(t *testing.T) {
	t.Parallel()

	got := HistoryKey("ns", "workload", autoscalingv1alpha1.ResourceMetricCPU)
	want := "ns/workload/cpu"
	if got != want {
		t.Fatalf("HistoryKey() = %q, want %q", got, want)
	}
}
