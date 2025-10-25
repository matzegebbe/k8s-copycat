package mirror

import (
	"path/filepath"
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
	var wildcard []wildcardAuthenticator
	for registry, authenticator := range creds {
		if authenticator == nil {
			continue
		}
		trimmed := strings.TrimSpace(registry)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.ContainsAny(lower, "*?[") {
			wildcard = append(wildcard, wildcardAuthenticator{pattern: lower, auth: authenticator})
			continue
		}
		normalized[lower] = authenticator
	}
	if len(normalized) == 0 && len(wildcard) == 0 {
		return &staticKeychain{}
	}
	return &staticKeychain{creds: normalized, wildcards: wildcard}
}

type wildcardAuthenticator struct {
	pattern string
	auth    authn.Authenticator
}

type staticKeychain struct {
	creds     map[string]authn.Authenticator
	wildcards []wildcardAuthenticator
}

func (s *staticKeychain) Resolve(resource authn.Resource) (authn.Authenticator, error) {
	if s == nil {
		return authn.Anonymous, nil
	}
	registry := strings.ToLower(strings.TrimSpace(resource.RegistryStr()))
	if auth, ok := s.creds[registry]; ok {
		return auth, nil
	}
	for _, wc := range s.wildcards {
		if matched, err := filepath.Match(wc.pattern, registry); err == nil && matched {
			return wc.auth, nil
		}
	}
	return authn.Anonymous, nil
}
