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
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/matzegebbe/doppler/internal/registry"
	"github.com/matzegebbe/doppler/pkg/util"
)

type Pusher interface {
	Mirror(ctx context.Context, sourceImage string) error
	DryRun() bool
}

type pusher struct {
	target  registry.Target
	dryRun  bool
	offline bool
}

func NewPusher(t registry.Target, dryRun, offline bool) Pusher {
	return &pusher{target: t, dryRun: dryRun, offline: offline}
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
	repo := util.CleanRepoName(srcRepo)
	if pref := p.target.RepoPrefix(); pref != "" {
		repo = strings.TrimSuffix(pref, "/") + "/" + repo
	}

	// Tag: keep if present, else digest-based using reference digest
	var tag string
	if t, ok := srcRef.(name.Tag); ok {
		tag = t.TagStr()
	} else {
		tag = "mirror-" + util.ShortDigest(srcRef.Identifier())
	}

	target := fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, tag)
	if p.offline {
		fmt.Printf("[OFFLINE] Would push image %s to %s\n", src, target)
		return nil
	}

	img, err := remote.Image(srcRef, remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure())))
	if err != nil {
		return fmt.Errorf("pull %s: %w", src, err)
	}

	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	auth := &authn.Basic{Username: username, Password: password}
	opts := []name.Option{name.WeakValidation}
	if p.target.Insecure() {
		opts = append(opts, name.Insecure)
	}
	targetRef, err := name.NewTag(target, opts...)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}

	// Skip if image already exists in target registry
	if _, err := remote.Head(targetRef, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure()))); err == nil {
		if p.dryRun {
			fmt.Printf("[DRY RUN] Image %s already present at %s\n", src, target)
		}
		return nil
	} else if te, ok := err.(*remotetransport.Error); ok && te.StatusCode == http.StatusNotFound {
		// continue to push
	} else if err != nil {
		return fmt.Errorf("check %s: %w", target, err)
	}

	if err := p.target.EnsureRepository(ctx, repo); err != nil {
		return fmt.Errorf("ensure repo %s: %w", repo, err)
	}

	if p.dryRun {
		fmt.Printf("[DRY RUN] Would push image %s to %s\n", src, target)
		return nil
	}
	if err := remote.Write(targetRef, img, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure()))); err != nil {
		return fmt.Errorf("push %s: %w", target, err)
	}
	return nil
}

func (p *pusher) DryRun() bool {
	return p.dryRun
}
