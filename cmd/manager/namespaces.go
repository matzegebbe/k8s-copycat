package main

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func validateAndExpandNamespaces(ctx context.Context, log logr.Logger, client kubernetes.Interface, selections []string) ([]string, error) {
	if len(selections) == 0 {
		return []string{"*"}, nil
	}

	normalized := make([]string, 0, len(selections))
	for _, raw := range selections {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
		if trimmed == "*" {
			return []string{"*"}, nil
		}
	}
	if len(normalized) == 0 {
		return []string{"*"}, nil
	}

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	existing := make([]string, 0, len(nsList.Items))
	existingSet := make(map[string]struct{}, len(nsList.Items))
	for _, item := range nsList.Items {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		existing = append(existing, name)
		existingSet[name] = struct{}{}
	}

	results := make(map[string]struct{}, len(normalized))
	for _, sel := range normalized {
		if hasWildcard(sel) {
			matches, matchErr := matchNamespacePattern(sel, existing)
			if matchErr != nil {
				log.Error(matchErr, "invalid namespace pattern", "pattern", sel)
				continue
			}
			if len(matches) == 0 {
				log.Info("namespace wildcard matched no namespaces", "pattern", sel)
				continue
			}
			for _, name := range matches {
				results[name] = struct{}{}
			}
			continue
		}

		if _, ok := existingSet[sel]; !ok {
			log.Error(fmt.Errorf("namespace %q does not exist", sel), "configured namespace missing", "namespace", sel)
		}
		results[sel] = struct{}{}
	}

	expanded := make([]string, 0, len(results))
	for name := range results {
		expanded = append(expanded, name)
	}
	sort.Strings(expanded)
	return expanded, nil
}

func hasWildcard(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

func matchNamespacePattern(pattern string, candidates []string) ([]string, error) {
	matches := make([]string, 0)
	for _, name := range candidates {
		ok, err := path.Match(pattern, name)
		if err != nil {
			return nil, err
		}
		if ok {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	return matches, nil
}
