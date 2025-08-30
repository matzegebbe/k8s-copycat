package registry

import "context"

// Target abstracts a destination registry (ECR or generic Docker registry).
type Target interface {
    Registry() string
    RepoPrefix() string
    EnsureRepository(ctx context.Context, name string) error
    BasicAuth(ctx context.Context) (username, password string, err error)
}
