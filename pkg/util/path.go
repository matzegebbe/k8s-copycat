package util

import (
	"regexp"
	"strings"
)

// PathMapping defines a replacement rule for repository paths. When Regex is
// set the From field is treated as a regular expression and replacement uses
// regexp.ReplaceAllString, otherwise a simple prefix substitution is applied.
type PathMapping struct {
	From  string `yaml:"from"`
	To    string `yaml:"to"`
	Regex bool   `yaml:"regex"`
}

type compiledMapping struct {
	PathMapping
	re *regexp.Regexp
}

// NewRepoPathTransformer returns a function that applies the given path
// mappings and then cleans the result for use in target registries. The first
// matching rule wins.
func NewRepoPathTransformer(mappings []PathMapping) func(string) string {
	compiled := make([]compiledMapping, 0, len(mappings))
	for _, m := range mappings {
		cm := compiledMapping{PathMapping: m}
		if m.Regex {
			if r, err := regexp.Compile(m.From); err == nil {
				cm.re = r
			} else {
				// skip invalid regex rules
				continue
			}
		}
		compiled = append(compiled, cm)
	}
	return func(p string) string {
		for _, m := range compiled {
			if m.Regex {
				if m.re.MatchString(p) {
					p = m.re.ReplaceAllString(p, m.To)
					break
				}
				continue
			}
			if strings.HasPrefix(p, m.From) {
				p = strings.TrimPrefix(p, m.From)
				if m.To != "" {
					p = strings.TrimSuffix(m.To, "/") + "/" + p
				}
				break
			}
		}
		return CleanRepoName(p)
	}
}
