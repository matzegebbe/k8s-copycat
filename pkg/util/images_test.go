package util

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestImagesFromPodSpecIncludesContainerNames(t *testing.T) {
	spec := &corev1.PodSpec{
		InitContainers: []corev1.Container{{
			Name:  "init-db",
			Image: "busybox:1",
		}},
		Containers: []corev1.Container{{
			Name:  "app",
			Image: "nginx:1",
		}, {
			Name:  "sidecar",
			Image: "busybox:1",
		}},
		EphemeralContainers: []corev1.EphemeralContainer{{
			EphemeralContainerCommon: corev1.EphemeralContainerCommon{
				Name:  "debug",
				Image: "alpine:3",
			},
		}},
	}

	images := ImagesFromPodSpec(spec)
	if len(images) != 4 {
		t.Fatalf("expected 4 images, got %d", len(images))
	}

	expected := []PodImage{
		{ContainerName: "init-db", Image: "busybox:1"},
		{ContainerName: "app", Image: "nginx:1"},
		{ContainerName: "sidecar", Image: "busybox:1"},
		{ContainerName: "debug", Image: "alpine:3"},
	}

	for i, want := range expected {
		if images[i] != want {
			t.Fatalf("index %d: expected %+v, got %+v", i, want, images[i])
		}
	}
}
