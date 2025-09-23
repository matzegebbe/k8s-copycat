package util

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

type PodImage struct {
	Image         string
	ContainerName string
}

func ImagesFromPodSpec(spec *corev1.PodSpec) []PodImage {
	if spec == nil {
		return nil
	}

	out := make([]PodImage, 0, len(spec.InitContainers)+len(spec.Containers)+len(spec.EphemeralContainers))

	add := func(name, img string) {
		img = strings.TrimSpace(img)
		name = strings.TrimSpace(name)
		if img == "" {
			return
		}
		out = append(out, PodImage{Image: img, ContainerName: name})
	}

	for _, c := range spec.InitContainers {
		add(c.Name, c.Image)
	}
	for _, c := range spec.Containers {
		add(c.Name, c.Image)
	}
	// Ephemeral containers (don’t forget these)
	for _, ec := range spec.EphemeralContainers {
		add(ec.Name, ec.Image)
	}

	return out
}

const maxRepoNameLength = 256

var repoAllowed = regexp.MustCompile(`[^a-z0-9_/.-]`)

func CleanRepoName(path string) string {
	p := strings.ToLower(strings.TrimPrefix(path, "/"))
	p = repoAllowed.ReplaceAllString(p, "-")
	p = strings.Trim(p, "-/.")
	if len(p) > maxRepoNameLength {
		hash := ShortDigest(p)
		// Leave room for the hash suffix to preserve uniqueness.
		keep := maxRepoNameLength - len(hash) - 1
		if keep < 0 {
			keep = 0
		}
		if keep > len(p) {
			keep = len(p)
		}
		trimmed := strings.TrimRight(p[:keep], "-/.")
		if trimmed == "" {
			p = hash
		} else {
			p = trimmed + "-" + hash
		}
		p = strings.Trim(p, "-/.")
		if len(p) > maxRepoNameLength {
			if len(hash) >= maxRepoNameLength {
				p = strings.Trim(hash[:maxRepoNameLength], "-/.")
			} else {
				p = strings.Trim(p[:maxRepoNameLength], "-/.")
			}
		}
	}
	if p == "" {
		p = "library/unknown"
	}
	return p
}

func ShortDigest(d string) string {
	h := sha1.Sum([]byte(d))
	return hex.EncodeToString(h[:])[:12]
}
