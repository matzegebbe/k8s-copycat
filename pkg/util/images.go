package util

import (
    "crypto/sha1"
    "encoding/hex"
    "regexp"
    "strings"

    corev1 "k8s.io/api/core/v1"
)

// ImagesFromPodSpec collects unique images from containers and initContainers.
func ImagesFromPodSpec(spec *corev1.PodSpec) []string {
    if spec == nil { return nil }
    set := map[string]struct{}{} ; add := func(s string){ s=strings.TrimSpace(s); if s!="" { set[s]=struct{}{} } }
    for _, c := range spec.InitContainers { add(c.Image) }
    for _, c := range spec.Containers { add(c.Image) }
    out := make([]string, 0, len(set))
    for k := range set { out = append(out, k) }
    return out
}

var repoAllowed = regexp.MustCompile(`[^a-z0-9_/.-]`)

// CleanRepoName makes a safe repo path for target registries.
func CleanRepoName(path string) string {
    p := strings.ToLower(strings.TrimPrefix(path, "/"))
    p = repoAllowed.ReplaceAllString(p, "-")
    p = strings.Trim(p, "-/.")
    if p == "" { p = "library/unknown" }
    return p
}

func ShortDigest(d string) string {
    h := sha1.Sum([]byte(d))
    return hex.EncodeToString(h[:])[:12]
}
