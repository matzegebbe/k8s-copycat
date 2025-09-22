package main

import (
	"reflect"
	"testing"
)

func TestResolveAllowedNamespaces(t *testing.T) {
	t.Parallel()

	got := resolveAllowedNamespaces("", nil)
	if !reflect.DeepEqual(got, []string{"*"}) {
		t.Fatalf("expected default wildcard, got %v", got)
	}

	got = resolveAllowedNamespaces("default, prod", nil)
	if !reflect.DeepEqual(got, []string{"default", "prod"}) {
		t.Fatalf("unexpected env parsing result: %v", got)
	}

	got = resolveAllowedNamespaces("   ", []string{"test", "prod"})
	if !reflect.DeepEqual(got, []string{"test", "prod"}) {
		t.Fatalf("expected fallback to config values, got %v", got)
	}
}

func TestResolveList(t *testing.T) {
	t.Parallel()

	got := resolveList("alpha , beta", []string{"gamma"})
	if !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("unexpected env list result: %v", got)
	}

	got = resolveList("", []string{"gamma", "delta"})
	if !reflect.DeepEqual(got, []string{"gamma", "delta"}) {
		t.Fatalf("expected fallback to config list, got %v", got)
	}

	got = resolveList("", []string{" ", "", "delta"})
	if !reflect.DeepEqual(got, []string{"delta"}) {
		t.Fatalf("expected sanitize to drop blanks, got %v", got)
	}
}
