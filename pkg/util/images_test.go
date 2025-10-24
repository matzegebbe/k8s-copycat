package util

import (
	"strings"
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

func TestCleanRepoNameStripsInvalidCharactersAndLength(t *testing.T) {
	repo := "Quay.io/Cilium/cilium-envoy:v1@sha256:318eff387835ca2717baab42a84f35a83a5f9e7d519253df87269f80b9ff0171"
	cleaned := CleanRepoName(repo)
	if strings.ContainsAny(cleaned, "@:") {
		t.Fatalf("cleaned repo still contains invalid characters: %q", cleaned)
	}
	if cleaned == "" {
		t.Fatal("expected cleaned repo to be non-empty")
	}
}

func TestImagesFromPodAnnotatesImageIDs(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  "init-db",
				Image: "busybox:1",
			}},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "nginx:1",
			}},
			EphemeralContainers: []corev1.EphemeralContainer{{
				EphemeralContainerCommon: corev1.EphemeralContainerCommon{
					Name:  "debug",
					Image: "alpine:3",
				},
			}},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:    "init-db",
				ImageID: "docker://registry.example.com/busybox@sha256:abc",
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:    "app",
				ImageID: "docker-pullable://registry.example.com/nginx@sha256:def",
			}},
			EphemeralContainerStatuses: []corev1.ContainerStatus{{
				Name:    "debug",
				ImageID: "registry.example.com/alpine@sha256:ghi",
			}},
		},
	}

	images := ImagesFromPod(pod)
	if len(images) != 3 {
		t.Fatalf("expected 3 images, got %d", len(images))
	}

	expected := map[string]string{
		"init-db": "registry.example.com/busybox@sha256:abc",
		"app":     "registry.example.com/nginx@sha256:def",
		"debug":   "registry.example.com/alpine@sha256:ghi",
	}

	for _, img := range images {
		want, ok := expected[img.ContainerName]
		if !ok {
			t.Fatalf("unexpected container %q", img.ContainerName)
		}
		if img.ImageID != want {
			t.Fatalf("container %q: expected imageID %q, got %q", img.ContainerName, want, img.ImageID)
		}
	}
}

func TestNormalizeImageIDHandlesPrefixes(t *testing.T) {
	cases := map[string]string{
		"docker://registry.example.com/repo@sha256:123":          "registry.example.com/repo@sha256:123",
		"docker-pullable://registry.example.com/repo@sha256:123": "registry.example.com/repo@sha256:123",
		"  registry.example.com/repo@sha256:123  ":               "registry.example.com/repo@sha256:123",
		"": "",
	}

	for input, want := range cases {
		if got := normalizeImageID(input); got != want {
			t.Fatalf("normalizeImageID(%q): expected %q, got %q", input, want, got)
		}
	}
}

func TestCleanRepoNameTruncatesLongRepositories(t *testing.T) {
	long := strings.Repeat("a", maxRepoNameLength+42)
	cleaned := CleanRepoName(long)
	if len(cleaned) > maxRepoNameLength {
		t.Fatalf("expected length <= %d, got %d", maxRepoNameLength, len(cleaned))
	}
	hashLen := len(ShortDigest(long))
	if len(cleaned) < hashLen {
		t.Fatalf("expected cleaned repo to be at least %d characters, got %d", hashLen, len(cleaned))
	}
	gotHash := cleaned[len(cleaned)-hashLen:]
	wantHash := ShortDigest(strings.ToLower(long))
	if gotHash != wantHash {
		t.Fatalf("expected hash suffix %q, got %q", wantHash, gotHash)
	}
}
