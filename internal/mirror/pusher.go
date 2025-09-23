package mirror

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Pusher interface {
	Mirror(ctx context.Context, sourceImage string, meta Metadata) error
	DryRun() bool
}

// Metadata captures contextual information about the image being mirrored.
type Metadata struct {
	Namespace     string
	PodName       string
	ContainerName string
}

type pusher struct {
	target          registry.Target
	dryRun          bool
	transform       func(string) string
	mu              sync.Mutex
	pushed          map[string]struct{}
	logger          logr.Logger
	keychain        authn.Keychain
	requestTimeout  time.Duration
	failureCooldown time.Duration
	failed          map[string]time.Time
	now             func() time.Time
}

const defaultFailureCooldown = 24 * time.Hour

var ErrInCooldown = errors.New("mirror: target is in failure cooldown")

type RetryError struct {
	Cause   error
	RetryAt time.Time
}

func (e *RetryError) Error() string {
	if e == nil {
		return ""
	}
	return e.Cause.Error()
}

func (e *RetryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewPusher(t registry.Target, dryRun bool, transform func(string) string, logger logr.Logger, keychain authn.Keychain, requestTimeout time.Duration) Pusher {
	if transform == nil {
		transform = util.CleanRepoName
	}
	if logger.GetSink() == nil {
		logger = ctrl.Log.WithName("mirror").WithName("pusher")
	} else {
		logger = logger.WithName("pusher")
	}
	if keychain == nil {
		keychain = NewStaticKeychain(nil)
	}
	if requestTimeout < 0 {
		requestTimeout = 0
	}
	return &pusher{
		target:          t,
		dryRun:          dryRun,
		transform:       transform,
		pushed:          make(map[string]struct{}),
		logger:          logger,
		keychain:        keychain,
		requestTimeout:  requestTimeout,
		failureCooldown: defaultFailureCooldown,
		failed:          make(map[string]time.Time),
		now:             time.Now,
	}
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

func (p *pusher) Mirror(ctx context.Context, src string, meta Metadata) error {
	log := logr.FromContextOrDiscard(ctx)
	if log.GetSink() == nil {
		log = p.logger
	}
	log = log.WithValues(
		"source", src,
		"namespace", meta.Namespace,
	)
	if meta.PodName != "" {
		log = log.WithValues("pod", meta.PodName)
	}
	if meta.ContainerName != "" {
		log = log.WithValues("container", meta.ContainerName)
	}

	srcRef, err := name.ParseReference(src, name.WeakValidation)
	if err != nil {
		return fmt.Errorf("parse source: %w", err)
	}

	// Build target repo path
	srcRepo := srcRef.Context().RepositoryStr()
	repo := p.resolveRepoPath(srcRepo, meta)

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
		stripped := src
		if idx := strings.Index(stripped, "@"); idx > 0 {
			stripped = stripped[:idx]
		}
		// Try to honour the original tag when the source reference included both tag and digest.
		if tagRef, tagErr := name.NewTag(stripped, name.WeakValidation); tagErr == nil {
			target = fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, tagRef.TagStr())
			targetRef, err = name.NewTag(target, opts...)
		} else {
			target = fmt.Sprintf("%s/%s@%s", p.target.Registry(), repo, r.DigestStr())
			targetRef, err = name.NewDigest(target, opts...)
		}
	default:
		return fmt.Errorf("unsupported reference type %T", srcRef)
	}
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}

	log = log.WithValues("target", target)

	if skip, err := p.beginProcessing(target, log); err != nil {
		log.Error(err, "unable to begin processing")
		return err
	} else if skip {
		return nil
	}

	log.V(1).Info("starting pull from source")

	pullCtx, cancelPull := p.operationContext(ctx)
	defer cancelPull()

	img, err := remote.Image(srcRef, remote.WithContext(pullCtx), remote.WithAuthFromKeychain(p.keychain), remote.WithTransport(transport(p.target.Insecure())))
	if err != nil {
		return p.failureResult(target, fmt.Errorf("pull %s: %w", src, err))
	}

	log.V(1).Info("finished pulling image from source")

	srcDigest, err := img.Digest()
	if err != nil {
		return p.failureResult(target, fmt.Errorf("digest %s: %w", src, err))
	}

	log.V(1).Info("resolving target registry credentials")

	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		return p.failureResult(target, fmt.Errorf("auth: %w", err))
	}

	if username != "" || password != "" {
		log.V(1).Info("using provided target registry credentials")
	} else {
		log.V(1).Info("no target registry credentials provided, using anonymous access")
	}

	auth := &authn.Basic{Username: username, Password: password}

	// Skip if image already exists in target registry with the same digest.
	headCtx, cancelHead := p.operationContext(ctx)
	desc, headErr := remote.Head(targetRef, remote.WithAuth(auth), remote.WithContext(headCtx), remote.WithTransport(transport(p.target.Insecure())))
	cancelHead()
	if headErr == nil {
		if desc.Digest == srcDigest {
			if p.dryRun {
				log.Info("image already present at target", "digest", srcDigest.String(), "dryRun", true, "result", "skipped")
			} else {
				log.Info("image already present at target", "digest", srcDigest.String())
			}
			return nil
		}
		log.Info("image already present with different digest, updating", "currentDigest", desc.Digest.String(), "sourceDigest", srcDigest.String())
	} else if te, ok := headErr.(*remotetransport.Error); ok && te.StatusCode == http.StatusNotFound {
		// continue to push
	} else if headErr != nil {
		return p.failureResult(target, fmt.Errorf("check %s: %w", target, headErr))
	}

	if err := p.target.EnsureRepository(ctx, repo); err != nil {
		return p.failureResult(target, fmt.Errorf("ensure repo %s: %w", repo, err))
	}

	if p.dryRun {
		log.Info("dry run: skipping push", "result", "skipped", "dryRun", true)
		return nil
	}
	log.Info("pushing image to target")
	pushCtx, cancelPush := p.operationContext(ctx)
	err = remote.Write(targetRef, img, remote.WithAuth(auth), remote.WithContext(pushCtx), remote.WithTransport(transport(p.target.Insecure())))
	cancelPush()
	if err != nil {
		return p.failureResult(target, fmt.Errorf("push %s: %w", target, err))
	}
	log.Info("finished pushing image")
	return nil
}

func (p *pusher) DryRun() bool {
	return p.dryRun
}

func (p *pusher) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.requestTimeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, p.requestTimeout)
}

func (p *pusher) beginProcessing(target string, log logr.Logger) (bool, error) {
	log.V(1).Info("evaluating processing state for target")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failureCooldown > 0 {
		if lastFailure, ok := p.failed[target]; ok {
			retryAt := lastFailure.Add(p.failureCooldown)
			now := p.now()
			if now.Before(retryAt) {
				err := &RetryError{Cause: ErrInCooldown, RetryAt: retryAt}
				log.Error(err, "skipping image due to previous failure", "retryAt", retryAt)
				return false, err
			}
			delete(p.failed, target)
		}
	}

	if _, exists := p.pushed[target]; exists {
		if p.dryRun {
			log.Info("image already processed during current run", "result", "skipped", "dryRun", true)
		} else {
			log.V(1).Info("image already processed during current run", "result", "skipped")
		}
		return true, nil
	}

	p.pushed[target] = struct{}{}
	return false, nil
}

func (p *pusher) failureResult(target string, cause error) error {
	now := p.now()
	retryAt := now.Add(p.failureCooldown)

	p.mu.Lock()
	delete(p.pushed, target)
	if p.failureCooldown > 0 {
		if p.failed == nil {
			p.failed = make(map[string]time.Time)
		}
		p.failed[target] = now
	}
	p.mu.Unlock()

	if p.failureCooldown > 0 {
		return &RetryError{Cause: cause, RetryAt: retryAt}
	}
	return cause
}

func (p *pusher) resolveRepoPath(srcRepo string, meta Metadata) string {
	cleaned := p.transform(srcRepo)
	prefix := expandRepoPrefix(p.target.RepoPrefix(), meta)
	if prefix == "" {
		return cleaned
	}
	if cleaned == "" {
		return util.CleanRepoName(prefix)
	}
	combined := strings.TrimSuffix(prefix, "/") + "/" + cleaned
	return util.CleanRepoName(combined)
}

func expandRepoPrefix(prefix string, meta Metadata) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"$namespace", meta.Namespace,
		"$podname", meta.PodName,
		"$container_name", meta.ContainerName,
	)
	expanded := replacer.Replace(prefix)
	expanded = strings.TrimSpace(expanded)
	if expanded == "" {
		return ""
	}
	segments := strings.Split(expanded, "/")
	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, "/")
}
