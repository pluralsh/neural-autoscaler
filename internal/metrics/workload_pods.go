package metrics

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

// ResolvePodNames lists pod names for the given target workload reference.
func ResolvePodNames(ctx context.Context, k8sClient client.Client, namespace string, ref autoscalingv1alpha1.CrossVersionObjectReference) ([]string, error) {
	resolver := &workloadPodResolver{
		k8sClient: k8sClient,
		namespace: namespace,
		targetRef: ref,
	}
	return resolver.resolve(ctx)
}

type workloadPodResolver struct {
	k8sClient client.Client
	namespace string
	targetRef autoscalingv1alpha1.CrossVersionObjectReference
}

func (r *workloadPodResolver) resolve(ctx context.Context) ([]string, error) {
	ref := r.targetRef
	switch ref.Kind {
	case "Pod":
		key := types.NamespacedName{Namespace: r.namespace, Name: ref.Name}
		pod := &corev1.Pod{}
		if err := r.k8sClient.Get(ctx, key, pod); err != nil {
			return nil, fmt.Errorf("get pod %q: %w", key, err)
		}
		return []string{ref.Name}, nil
	case "Deployment":
		return r.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			dep := &appsv1.Deployment{}
			if err := r.k8sClient.Get(ctx, key, dep); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(dep.Spec.Selector)
		})
	case "StatefulSet":
		return r.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			sts := &appsv1.StatefulSet{}
			if err := r.k8sClient.Get(ctx, key, sts); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(sts.Spec.Selector)
		})
	case "ReplicaSet":
		return r.podNamesForSelector(ctx, func(ctx context.Context, key types.NamespacedName) (labels.Selector, error) {
			rs := &appsv1.ReplicaSet{}
			if err := r.k8sClient.Get(ctx, key, rs); err != nil {
				return nil, err
			}
			return metav1.LabelSelectorAsSelector(rs.Spec.Selector)
		})
	default:
		return nil, fmt.Errorf("unsupported targetRef kind %q", ref.Kind)
	}
}

type selectorResolver func(ctx context.Context, key types.NamespacedName) (labels.Selector, error)

func (r *workloadPodResolver) podNamesForSelector(ctx context.Context, resolve selectorResolver) ([]string, error) {
	key := types.NamespacedName{Namespace: r.namespace, Name: r.targetRef.Name}
	selector, err := resolve(ctx, key)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get target %s %q: %w", r.targetRef.Kind, key, err)
		}
		return nil, fmt.Errorf("resolve selector for %s %q: %w", r.targetRef.Kind, key, err)
	}

	podList := &corev1.PodList{}
	if err := r.k8sClient.List(ctx, podList, client.InNamespace(r.namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("list pods for %s %q: %w", r.targetRef.Kind, key, err)
	}

	names := make([]string, 0, len(podList.Items))
	for _, pod := range podList.Items {
		names = append(names, pod.Name)
	}
	return names, nil
}

// PodMatcher returns a PromQL pod label matcher for cAdvisor queries.
// Exact pod names are joined with | when known; otherwise a workload name pattern is used.
func PodMatcher(podNames []string, ref autoscalingv1alpha1.CrossVersionObjectReference) (label string, exact bool) {
	if ref.Kind == "Pod" && len(podNames) == 1 {
		return podNames[0], true
	}
	if len(podNames) > 0 {
		return strings.Join(podNames, "|"), false
	}
	return ref.Name + "-.*", false
}
