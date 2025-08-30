package mirror

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/matzegebbe/doppler/internal/registry"
	"github.com/matzegebbe/doppler/pkg/util"
)

type Pusher interface {
	Mirror(ctx context.Context, sourceImage string) error
}

type pusher struct {
	target registry.Target
	dryRun bool
}

func NewPusher(t registry.Target, dryRun bool) Pusher {
	return &pusher{target: t, dryRun: dryRun}
}

func transport() http.RoundTripper {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           d.DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

func (p *pusher) Mirror(ctx context.Context, src string) error {
	srcRef, err := name.ParseReference(src, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parse source: %w", err)
	}

	img, err := remote.Image(srcRef, remote.WithContext(ctx), remote.WithTransport(transport()))
	if err != nil {
		return fmt.Errorf("pull %s: %w", src, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return fmt.Errorf("digest: %w", err)
	}

	// Build target repo path
	srcRepo := srcRef.Context().RepositoryStr()
	repo := util.CleanRepoName(srcRepo)
	if pref := p.target.RepoPrefix(); pref != "" {
		repo = strings.TrimSuffix(pref, "/") + "/" + repo
	}

	if err := p.target.EnsureRepository(ctx, repo); err != nil {
		return fmt.Errorf("ensure repo %s: %w", repo, err)
	}

	// Tag: keep if present, else digest-based
	var tag string
	if t, ok := srcRef.(name.Tag); ok {
		tag = t.TagStr()
	} else {
		tag = "mirror-" + util.ShortDigest(digest.String())
	}

	target := fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, tag)
	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	auth := &authn.Basic{Username: username, Password: password}
	targetRef, err := name.NewTag(target, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}

	if p.dryRun {
		fmt.Printf("[DRY RUN] Would push image %s to %s\n", src, target)
		return nil
	}
	if err := remote.Write(targetRef, img, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport())); err != nil {
		return fmt.Errorf("push %s: %w", target, err)
	}
	return nil
}
