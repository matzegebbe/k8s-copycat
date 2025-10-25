package main

import (
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"

	"github.com/matzegebbe/k8s-copycat/internal/config"
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

	got = resolveList("", []string{"foo, bar", "baz"})
	if !reflect.DeepEqual(got, []string{"foo", "bar", "baz"}) {
		t.Fatalf("expected comma-delimited config values to be split, got %v", got)
	}
}

func TestBuildKeychainFromConfigRegistryAliases(t *testing.T) {
	t.Parallel()

	creds := []config.RegistryCredential{{
		Registry:        "registry-1.docker.io",
		RegistryAliases: []string{"index.docker.io", "docker.io", "mirror.docker.internal"},
		Username:        "user",
		Password:        "pass",
	}}

	kc := buildKeychainFromConfig(creds)
	if kc == nil {
		t.Fatalf("expected keychain, got nil")
	}

	for _, registry := range []string{"registry-1.docker.io", "index.docker.io", "docker.io", "mirror.docker.internal"} {
		auth, err := kc.Resolve(fakeResource{registry: registry})
		if err != nil {
			t.Fatalf("resolve %s: %v", registry, err)
		}
		basic, ok := auth.(*authn.Basic)
		if !ok {
			t.Fatalf("expected basic auth for %s, got %T", registry, auth)
		}
		if basic.Username != "user" || basic.Password != "pass" {
			t.Fatalf("unexpected credentials for %s: %+v", registry, basic)
		}
	}
}

func TestBuildKeychainFromConfigCustomAliasesAndWildcard(t *testing.T) {
	t.Parallel()

	creds := []config.RegistryCredential{
		{
			Registry:        "ghcr.io",
			RegistryAliases: []string{"*.ghcr.io", "docker.pkg.github.com"},
			Token:           "ghcr-token",
		},
	}

	kc := buildKeychainFromConfig(creds)
	if kc == nil {
		t.Fatalf("expected keychain, got nil")
	}

	tests := map[string]string{
		"ghcr.io":               "ghcr-token",
		"packages.ghcr.io":      "ghcr-token",
		"docker.pkg.github.com": "ghcr-token",
	}

	for registry, expectedToken := range tests {
		auth, err := kc.Resolve(fakeResource{registry: registry})
		if err != nil {
			t.Fatalf("resolve %s: %v", registry, err)
		}
		cfg, err := auth.Authorization()
		if err != nil {
			t.Fatalf("authorization for %s: %v", registry, err)
		}
		if cfg == nil || cfg.RegistryToken != expectedToken {
			t.Fatalf("unexpected token for %s: %+v", registry, cfg)
		}
	}

	if auth, _ := kc.Resolve(fakeResource{registry: "ghcr.example.com"}); auth != authn.Anonymous {
		t.Fatalf("expected anonymous auth for ghcr.example.com")
	}
}

type fakeResource struct {
	registry string
}

func (f fakeResource) String() string {
	return f.registry
}

func (f fakeResource) RegistryStr() string {
	return f.registry
}
