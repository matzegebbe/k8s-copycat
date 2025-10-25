package mirror

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
)

func TestStaticKeychainWildcardMatch(t *testing.T) {
	t.Parallel()

	kc := NewStaticKeychain(map[string]authn.Authenticator{
		"example.com":    &authn.Basic{Username: "user", Password: "pass"},
		"*.mirror.local": authn.FromConfig(authn.AuthConfig{RegistryToken: "token"}),
	})

	auth, err := kc.Resolve(testResource("example.com"))
	if err != nil {
		t.Fatalf("resolve example.com: %v", err)
	}
	if _, ok := auth.(*authn.Basic); !ok {
		t.Fatalf("expected basic auth for example.com, got %T", auth)
	}

	auth, err = kc.Resolve(testResource("cache.mirror.local"))
	if err != nil {
		t.Fatalf("resolve cache.mirror.local: %v", err)
	}
	cfg, err := auth.Authorization()
	if err != nil {
		t.Fatalf("authorization for wildcard: %v", err)
	}
	if cfg == nil || cfg.RegistryToken != "token" {
		t.Fatalf("unexpected token for wildcard: %+v", cfg)
	}

	if auth, _ = kc.Resolve(testResource("no-match.local")); auth != authn.Anonymous {
		t.Fatalf("expected anonymous auth for no-match.local")
	}
}

type testResource string

func (t testResource) String() string {
	return string(t)
}

func (t testResource) RegistryStr() string {
	return string(t)
}
