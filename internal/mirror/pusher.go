package mirror

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
)

type Pusher interface {
	Mirror(ctx context.Context, sourceImage string) error
	DryRun() bool
}

type pusher struct {
	target    registry.Target
	dryRun    bool
	offline   bool
	transform func(string) string
	mu        sync.Mutex
	pushed    map[string]struct{}
}

func NewPusher(t registry.Target, dryRun, offline bool, transform func(string) string) Pusher {
	if transform == nil {
		transform = util.CleanRepoName
	}
	return &pusher{target: t, dryRun: dryRun, offline: offline, transform: transform, pushed: make(map[string]struct{})}
}

func transport(insecure bool) http.RoundTripper {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		tlsCfg.InsecureSkipVerify = true
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsCfg,
	}
}

func (p *pusher) Mirror(ctx context.Context, src string) error {
	srcRef, err := name.ParseReference(src, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parse source: %w", err)
	}

	// Build target repo path
	srcRepo := srcRef.Context().RepositoryStr()
	repo := p.transform(srcRepo)
	if pref := p.target.RepoPrefix(); pref != "" {
		repo = strings.TrimSuffix(pref, "/") + "/" + repo
	}

	var target string
	var targetRef name.Reference
	opts := []name.Option{name.WeakValidation}
	if p.target.Insecure() {
		opts = append(opts, name.Insecure)
	}
	switch r := srcRef.(type) {
	case name.Tag:
		target = fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, r.TagStr())
		targetRef, err = name.NewTag(target, opts...)
	case name.Digest:
		target = fmt.Sprintf("%s/%s@%s", p.target.Registry(), repo, r.DigestStr())
		targetRef, err = name.NewDigest(target, opts...)
	default:
		return fmt.Errorf("unsupported reference type %T", srcRef)
	}
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}

	p.mu.Lock()
	if _, exists := p.pushed[target]; exists {
		p.mu.Unlock()
		if p.dryRun {
			fmt.Printf("[DRY RUN] Image %s already processed this run\n", src)
		}
		return nil
	}
	p.pushed[target] = struct{}{}
	p.mu.Unlock()

	if p.offline {
		fmt.Printf("[OFFLINE] Would push image %s to %s\n", src, target)
		return nil
	}

	img, err := remote.Image(srcRef, remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure())))
	if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("pull %s: %w", src, err)
	}

	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("auth: %w", err)
	}

	auth := &authn.Basic{Username: username, Password: password}

	// Skip if image already exists in target registry
	if _, err := remote.Head(targetRef, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure()))); err == nil {
		if p.dryRun {
			fmt.Printf("[DRY RUN] Image %s already present at %s\n", src, target)
		}
		return nil
	} else if te, ok := err.(*remotetransport.Error); ok && te.StatusCode == http.StatusNotFound {
		// continue to push
	} else if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("check %s: %w", target, err)
	}

	if err := p.target.EnsureRepository(ctx, repo); err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("ensure repo %s: %w", repo, err)
	}

	if p.dryRun {
		fmt.Printf("[DRY RUN] Would push image %s to %s\n", src, target)
		return nil
	}
	if err := remote.Write(targetRef, img, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure()))); err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("push %s: %w", target, err)
	}
	return nil
}

func (p *pusher) DryRun() bool {
	return p.dryRun
}
