package mirror

import (
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
)

// NewStaticKeychain builds a simple keychain for authenticating against specific
// registries. Registry hostnames are matched case-insensitively.
func NewStaticKeychain(creds map[string]authn.Authenticator) authn.Keychain {
	if len(creds) == 0 {
		return &staticKeychain{}
	}
	normalized := make(map[string]authn.Authenticator, len(creds))
	for registry, authenticator := range creds {
		if authenticator == nil {
			continue
		}
		trimmed := strings.TrimSpace(registry)
		if trimmed == "" {
			continue
		}
		normalized[strings.ToLower(trimmed)] = authenticator
	}
	if len(normalized) == 0 {
		return &staticKeychain{}
	}
	return &staticKeychain{creds: normalized}
}

type staticKeychain struct {
	creds map[string]authn.Authenticator
}

func (s *staticKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	if s == nil {
		return authn.Anonymous, nil
	}
	registry := strings.ToLower(strings.TrimSpace(resource.RegistryStr()))
	if auth, ok := s.creds[registry]; ok {
		return auth, nil
	}
	return authn.Anonymous, nil
}
