package util

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func ImagesFromPodSpec(spec *corev1.PodSpec) []string {
	if spec == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(spec.InitContainers)+len(spec.Containers))
	out := make([]string, 0, len(spec.InitContainers)+len(spec.Containers))

	add := func(img string) {
		img = strings.TrimSpace(img)
		if img == "" {
			return
		}
		if _, ok := seen[img]; ok {
			return
		}
		seen[img] = struct{}{}
		out = append(out, img)
	}

	for _, c := range spec.InitContainers {
		add(c.Image)
	}
	for _, c := range spec.Containers {
		add(c.Image)
	}
	// Ephemeral containers (don’t forget these)
	for _, ec := range spec.EphemeralContainers {
		add(ec.Image)
	}

	return out
}

var repoAllowed = regexp.MustCompile(`[^a-z0-9_/.-]`)

func CleanRepoName(path string) string {
	p := strings.ToLower(strings.TrimPrefix(path, "/"))
	p = repoAllowed.ReplaceAllString(p, "-")
	p = strings.Trim(p, "-/.")
	if p == "" {
		p = "library/unknown"
	}
	return p
}

func ShortDigest(d string) string {
	h := sha1.Sum([]byte(d))
	return hex.EncodeToString(h[:])[:12]
}
