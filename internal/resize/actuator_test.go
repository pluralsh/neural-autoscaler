package resize

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	autoscalingv1alpha1 "github.com/pluralsh/neural-autoscaler/api/v1alpha1"
)

func TestBuildResizePodUpdatesOnlyTargetContainer(t *testing.T) {
	t.Parallel()

	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
				{
					Name: "sidecar",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}
	targets := TargetResult{
		CPU:    quantityPtr(resource.MustParse("300m")),
		Memory: quantityPtr(resource.MustParse("512Mi")),
	}
	resources := map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricCPU):    {},
		string(autoscalingv1alpha1.ResourceMetricMemory): {},
	}

	resized := buildResizePod(pod, []containerResizePlan{
		{
			Index:   0,
			Name:    "app",
			Current: pod.Spec.Containers[0].Resources.Requests,
			Targets: targets,
		},
	}, resources)

	app := resized.Spec.Containers[0]
	gotAppCPUReq := app.Resources.Requests[corev1.ResourceCPU]
	if (&gotAppCPUReq).Cmp(resource.MustParse("300m")) != 0 {
		t.Fatalf("app cpu request = %s, want 300m", (&gotAppCPUReq).String())
	}
	gotAppMemReq := app.Resources.Requests[corev1.ResourceMemory]
	if (&gotAppMemReq).Cmp(resource.MustParse("512Mi")) != 0 {
		t.Fatalf("app memory request = %s, want 512Mi", (&gotAppMemReq).String())
	}
	gotAppCPULim := app.Resources.Limits[corev1.ResourceCPU]
	if (&gotAppCPULim).Cmp(resource.MustParse("300m")) != 0 {
		t.Fatalf("app cpu limit = %s, want 300m", (&gotAppCPULim).String())
	}
	gotAppMemLim := app.Resources.Limits[corev1.ResourceMemory]
	if (&gotAppMemLim).Cmp(resource.MustParse("512Mi")) != 0 {
		t.Fatalf("app memory limit = %s, want 512Mi", (&gotAppMemLim).String())
	}

	sidecar := resized.Spec.Containers[1]
	gotSidecarCPUReq := sidecar.Resources.Requests[corev1.ResourceCPU]
	if (&gotSidecarCPUReq).Cmp(resource.MustParse("50m")) != 0 {
		t.Fatalf("sidecar cpu request changed to %s, want 50m", (&gotSidecarCPUReq).String())
	}
	gotSidecarMemReq := sidecar.Resources.Requests[corev1.ResourceMemory]
	if (&gotSidecarMemReq).Cmp(resource.MustParse("64Mi")) != 0 {
		t.Fatalf("sidecar memory request changed to %s, want 64Mi", (&gotSidecarMemReq).String())
	}
	gotSidecarCPULim := sidecar.Resources.Limits[corev1.ResourceCPU]
	if (&gotSidecarCPULim).Cmp(resource.MustParse("100m")) != 0 {
		t.Fatalf("sidecar cpu limit changed to %s, want 100m", (&gotSidecarCPULim).String())
	}
	gotSidecarMemLim := sidecar.Resources.Limits[corev1.ResourceMemory]
	if (&gotSidecarMemLim).Cmp(resource.MustParse("128Mi")) != 0 {
		t.Fatalf("sidecar memory limit changed to %s, want 128Mi", (&gotSidecarMemLim).String())
	}
}

func TestBuildResizePodUpdatesAllContainersForWildcardPlan(t *testing.T) {
	t.Parallel()

	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
				{
					Name: "sidecar",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
		},
	}
	resources := map[string]autoscalingv1alpha1.ResourceBoundsSpec{
		string(autoscalingv1alpha1.ResourceMetricCPU):    {},
		string(autoscalingv1alpha1.ResourceMetricMemory): {},
	}
	resized := buildResizePod(pod, []containerResizePlan{
		{
			Index:   0,
			Name:    "app",
			Current: pod.Spec.Containers[0].Resources.Requests,
			Targets: TargetResult{
				CPU:    quantityPtr(resource.MustParse("200m")),
				Memory: quantityPtr(resource.MustParse("256Mi")),
			},
		},
		{
			Index:   1,
			Name:    "sidecar",
			Current: pod.Spec.Containers[1].Resources.Requests,
			Targets: TargetResult{
				CPU:    quantityPtr(resource.MustParse("75m")),
				Memory: quantityPtr(resource.MustParse("96Mi")),
			},
		},
	}, resources)

	gotAppCPU := resized.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
	if (&gotAppCPU).Cmp(resource.MustParse("200m")) != 0 {
		t.Fatalf("app cpu request = %s, want 200m", (&gotAppCPU).String())
	}
	gotSidecarCPU := resized.Spec.Containers[1].Resources.Requests[corev1.ResourceCPU]
	if (&gotSidecarCPU).Cmp(resource.MustParse("75m")) != 0 {
		t.Fatalf("sidecar cpu request = %s, want 75m", (&gotSidecarCPU).String())
	}
}

func TestTargetContainerIndexes(t *testing.T) {
	t.Parallel()

	t.Run("no containers", func(t *testing.T) {
		t.Parallel()
		if indexes := targetContainerIndexes(corev1.Pod{}, &autoscalingv1alpha1.ResizeSpec{}); len(indexes) != 0 {
			t.Fatalf("expected no target containers, got %v", indexes)
		}
	})

	t.Run("returns first index when container name unset", func(t *testing.T) {
		t.Parallel()
		pod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
			},
		}
		indexes := targetContainerIndexes(pod, &autoscalingv1alpha1.ResizeSpec{})
		if len(indexes) != 1 || indexes[0] != 0 {
			t.Fatalf("target indexes = %v, want [0]", indexes)
		}
	})

	t.Run("returns matching named container", func(t *testing.T) {
		t.Parallel()
		pod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
			},
		}
		indexes := targetContainerIndexes(pod, &autoscalingv1alpha1.ResizeSpec{
			ContainerName: strPtr("sidecar"),
		})
		if len(indexes) != 1 || indexes[0] != 1 {
			t.Fatalf("target indexes = %v, want [1]", indexes)
		}
	})

	t.Run("wildcard selects all containers", func(t *testing.T) {
		t.Parallel()
		pod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
			},
		}
		indexes := targetContainerIndexes(pod, &autoscalingv1alpha1.ResizeSpec{
			ContainerName: strPtr("*"),
		})
		if len(indexes) != 2 || indexes[0] != 0 || indexes[1] != 1 {
			t.Fatalf("target indexes = %v, want [0 1]", indexes)
		}
	})

	t.Run("missing named container", func(t *testing.T) {
		t.Parallel()
		pod := corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "app"}, {Name: "sidecar"}},
			},
		}
		if indexes := targetContainerIndexes(pod, &autoscalingv1alpha1.ResizeSpec{
			ContainerName: strPtr("metrics"),
		}); len(indexes) != 0 {
			t.Fatalf("expected no target container, got %v", indexes)
		}
	})
}
