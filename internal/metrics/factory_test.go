package metrics

import (
	"testing"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFactoryNewFetcherPrometheus(t *testing.T) {
	t.Parallel()

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "api"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
			},
		},
	}
	factory := &Factory{K8sClient: fake.NewClientBuilder().WithObjects(dep).Build()}
	spec := autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourcePrometheus,
		Prometheus: &autoscalingv1alpha1.PrometheusSourceSpec{
			URL: "http://prometheus:9090",
			TargetRef: &autoscalingv1alpha1.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "api",
			},
			Resources: []autoscalingv1alpha1.ResourceMetric{autoscalingv1alpha1.ResourceMetricCPU},
		},
	}

	fetcher, err := factory.NewFetcher(spec, "default")
	if err != nil {
		t.Fatalf("NewFetcher() error = %v", err)
	}
	if fetcher == nil {
		t.Fatal("expected prometheus fetcher")
	}
}

func TestFactoryNewFetcherPrometheusRequiresK8sClientForTargetRef(t *testing.T) {
	t.Parallel()

	factory := &Factory{}
	_, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourcePrometheus,
		Prometheus: &autoscalingv1alpha1.PrometheusSourceSpec{
			URL: "http://prometheus:9090",
			TargetRef: &autoscalingv1alpha1.CrossVersionObjectReference{
				Kind: "Deployment",
				Name: "api",
			},
			Resources: []autoscalingv1alpha1.ResourceMetric{autoscalingv1alpha1.ResourceMetricCPU},
		},
	}, "default")
	if err == nil {
		t.Fatal("expected error when kubernetes client is missing for targetRef")
	}
}

func TestFactoryNewFetcherMetricsServerRequiresClients(t *testing.T) {
	t.Parallel()

	factory := &Factory{}
	_, err := factory.NewFetcher(autoscalingv1alpha1.MetricsSourceSpec{
		Type: autoscalingv1alpha1.MetricsSourceMetricsServer,
		MetricsServer: &autoscalingv1alpha1.MetricsServerSourceSpec{
			TargetRef: autoscalingv1alpha1.CrossVersionObjectReference{Kind: "Pod", Name: "api-1"},
			Resources: []autoscalingv1alpha1.ResourceMetric{autoscalingv1alpha1.ResourceMetricCPU},
		},
	}, "default")
	if err == nil {
		t.Fatal("expected error when metrics server clients are missing")
	}
}
