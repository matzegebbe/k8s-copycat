package mirror

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sort"
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

type ErrThrottled struct {
	Target string
	Wait   time.Duration
}

func (e ErrThrottled) Error() string {
	return fmt.Sprintf("throttled push for %s; retry in %s", e.Target, e.Wait)
}

type pushRecord struct {
	digest   string
	lastPush time.Time
	inflight bool
}

type pusher struct {
	target       registry.Target
	dryRun       bool
	offline      bool
	transform    func(string) string
	pushInterval time.Duration
	mu           sync.Mutex
	pushed       map[string]pushRecord
}

// CacheEntry describes a cached target reference and the digest recorded for it.
type CacheEntry struct {
	Target   string    `json:"target"`
	Digest   string    `json:"digest"`
	LastPush time.Time `json:"lastPush"`
	Inflight bool      `json:"inflight"`
}

func NewPusher(t registry.Target, dryRun, offline bool, transform func(string) string, interval time.Duration) Pusher {
	if transform == nil {
		transform = util.CleanRepoName
	}
	if interval < 0 {
		interval = 0
	}
	return &pusher{target: t, dryRun: dryRun, offline: offline, transform: transform, pushInterval: interval, pushed: make(map[string]pushRecord)}
}

// ResetCache removes all cached push records and returns the targets that were cleared.
func (p *pusher) ResetCache() []string {
	p.mu.Lock()
	removed := make([]string, 0, len(p.pushed))
	for target := range p.pushed {
		removed = append(removed, target)
	}
	p.pushed = make(map[string]pushRecord)
	p.mu.Unlock()

	sort.Strings(removed)
	return removed
}

// Evict removes an exact target reference from the cache.
func (p *pusher) Evict(target string) bool {
	if target == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pushed[target]; ok {
		delete(p.pushed, target)
		return true
	}
	return false
}

// EvictPrefix removes cached entries whose target matches the provided prefix and
// returns the evicted targets in lexical order.
func (p *pusher) EvictPrefix(prefix string) []string {
	if prefix == "" {
		return nil
	}
	p.mu.Lock()
	removed := make([]string, 0)
	for target := range p.pushed {
		if strings.HasPrefix(target, prefix) {
			delete(p.pushed, target)
			removed = append(removed, target)
		}
	}
	p.mu.Unlock()

	sort.Strings(removed)
	return removed
}

// CacheEntries returns a snapshot of the cached push records for observability purposes.
func (p *pusher) CacheEntries() []CacheEntry {
	p.mu.Lock()
	entries := make([]CacheEntry, 0, len(p.pushed))
	for target, rec := range p.pushed {
		entries = append(entries, CacheEntry{Target: target, Digest: rec.digest, LastPush: rec.lastPush, Inflight: rec.inflight})
	}
	p.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Target < entries[j].Target
	})
	return entries
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

	throttleWait := func() time.Duration {
		if p.pushInterval > 0 {
			return p.pushInterval
		}
		return time.Second
	}

	p.mu.Lock()
	rec := p.pushed[target]
	if rec.inflight {
		wait := throttleWait()
		p.mu.Unlock()
		fmt.Printf("Throttled concurrent push for %s to %s; retry in %s\n", src, target, wait)
		return ErrThrottled{Target: target, Wait: wait}
	}
	if p.pushInterval > 0 && !rec.lastPush.IsZero() {
		if elapsed := time.Since(rec.lastPush); elapsed < p.pushInterval {
			wait := p.pushInterval - elapsed
			p.mu.Unlock()
			if p.dryRun {
				fmt.Printf("[DRY RUN] Throttled push for %s to %s; retry in %s\n", src, target, wait)
			} else {
				fmt.Printf("Throttled push for %s to %s; retry in %s\n", src, target, wait)
			}
			return ErrThrottled{Target: target, Wait: wait}
		}
	}
	rec.inflight = true
	p.pushed[target] = rec
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		if current, ok := p.pushed[target]; ok {
			current.inflight = false
			p.pushed[target] = current
		}
		p.mu.Unlock()
	}()

	if p.offline {
		fmt.Printf("[OFFLINE] Would push image %s to %s\n", src, target)
		p.mu.Lock()
		rec := p.pushed[target]
		rec.lastPush = time.Now()
		p.pushed[target] = rec
		p.mu.Unlock()
		return nil
	}

	img, err := remote.Image(srcRef, remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure())))
	if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("pull %s: %w", src, err)
	}

	srcDigest, err := img.Digest()
	if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("digest %s: %w", src, err)
	}
	srcDigestStr := srcDigest.String()

	p.mu.Lock()
	rec = p.pushed[target]
	if rec.digest == srcDigestStr {
		rec.lastPush = time.Now()
		p.pushed[target] = rec
		p.mu.Unlock()
		if p.dryRun {
			fmt.Printf("[DRY RUN] Image %s already processed with digest %s this run\n", src, srcDigestStr)
		} else {
			fmt.Printf("Image %s already processed with digest %s this run\n", src, srcDigestStr)
		}
		return nil
	}
	p.mu.Unlock()

	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("auth: %w", err)
	}

	auth := &authn.Basic{Username: username, Password: password}

	desc, err := remote.Head(targetRef, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure())))
	if err == nil {
		if desc.Digest == srcDigest {
			p.mu.Lock()
			rec = p.pushed[target]
			rec.digest = srcDigestStr
			rec.lastPush = time.Now()
			p.pushed[target] = rec
			p.mu.Unlock()
			if p.dryRun {
				fmt.Printf("[DRY RUN] Image %s already present at %s (digest %s)\n", src, target, srcDigestStr)
			} else {
				fmt.Printf("Image %s already present at %s (digest %s)\n", src, target, srcDigestStr)
			}
			return nil
		}
		fmt.Printf("Updating %s at %s (digest %s -> %s)\n", src, target, desc.Digest.String(), srcDigestStr)
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
		fmt.Printf("[DRY RUN] Would push image %s (digest %s) to %s\n", src, srcDigestStr, target)
		p.mu.Lock()
		rec = p.pushed[target]
		rec.lastPush = time.Now()
		p.pushed[target] = rec
		p.mu.Unlock()
		return nil
	}

	fmt.Printf("Pushing image %s (digest %s) to %s\n", src, srcDigestStr, target)
	if err := remote.Write(targetRef, img, remote.WithAuth(auth), remote.WithContext(ctx), remote.WithTransport(transport(p.target.Insecure()))); err != nil {
		p.mu.Lock()
		delete(p.pushed, target)
		p.mu.Unlock()
		return fmt.Errorf("push %s: %w", target, err)
	}
	fmt.Printf("Finished pushing image %s (digest %s) to %s\n", src, srcDigestStr, target)

	p.mu.Lock()
	rec = p.pushed[target]
	rec.digest = srcDigestStr
	rec.lastPush = time.Now()
	p.pushed[target] = rec
	p.mu.Unlock()

	return nil
}

func (p *pusher) DryRun() bool {
	return p.dryRun
}
