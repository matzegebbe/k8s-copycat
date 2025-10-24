package mirror

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/metrics"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Pusher interface {
	Mirror(ctx context.Context, sourceImage string, meta Metadata) error
	DryRun() bool
	DryPull() bool
	ResetCooldown() (cleared int, cooldownEnabled bool)
}

// Metadata captures contextual information about the image being mirrored.
type Metadata struct {
	Namespace     string
	PodName       string
	ContainerName string
}

type pusher struct {
	target                     registry.Target
	dryRun                     bool
	dryPull                    bool
	transform                  func(string) string
	pullByDigest               bool
	allowDifferentDigestRepush bool
	mu                         sync.Mutex
	pushed                     map[string]struct{}
	logger                     logr.Logger
	keychain                   authn.Keychain
	requestTimeout             time.Duration
	failureCooldown            time.Duration
	failed                     map[string]time.Time
	now                        func() time.Time
	excludedRegistries         []string
}

const DefaultFailureCooldown = 24 * time.Hour

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

func NewPusher(t registry.Target, dryRun bool, dryPull bool, transform func(string) string, logger logr.Logger, keychain authn.Keychain, requestTimeout time.Duration, failureCooldown time.Duration, pullByDigest bool, allowDifferentDigestRepush bool, excluded []string) Pusher {
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
	if failureCooldown < 0 {
		failureCooldown = DefaultFailureCooldown
	}
	normalizedExclusions := normalizeExcludedRegistries(excluded)

	return &pusher{
		target:                     t,
		dryRun:                     dryRun,
		dryPull:                    dryPull,
		transform:                  transform,
		pullByDigest:               pullByDigest,
		allowDifferentDigestRepush: allowDifferentDigestRepush,
		pushed:                     make(map[string]struct{}),
		logger:                     logger,
		keychain:                   keychain,
		requestTimeout:             requestTimeout,
		failureCooldown:            failureCooldown,
		failed:                     make(map[string]time.Time),
		now:                        time.Now,
		excludedRegistries:         normalizedExclusions,
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

	if excluded, ok := p.matchExcludedRegistry(src); ok {
		log.Info(
			"source matches excluded registry prefix, skipping",
			"excludedPrefix", excluded,
			"result", "skipped",
		)
		return nil
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
	pullRef := srcRef
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

	log.V(1).Info("resolved target reference", "reference", target)

	if skip, err := p.beginProcessing(target, log); err != nil {
		log.Error(err, "unable to begin processing")
		return err
	} else if skip {
		return nil
	}

	getDescriptor := func(ref name.Reference) (*remote.Descriptor, context.CancelFunc, error) {
		descCtx, cancel := p.operationContext(ctx)
		desc, err := remote.Get(
			ref,
			remote.WithContext(descCtx),
			remote.WithAuthFromKeychain(p.keychain),
			remote.WithTransport(transport(p.target.Insecure())),
		)
		if err != nil {
			cancel()
			return nil, func() {}, err
		}
		return desc, cancel, nil
	}

	var desc *remote.Descriptor
	descCancel := func() {}

	if p.pullByDigest {
		if tagRef, ok := srcRef.(name.Tag); ok {
			sourceDesc, cancelSource, digestErr := getDescriptor(tagRef)
			if digestErr != nil {
				logRegistryAuthError(log, digestErr, "digest resolution")
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("resolve digest %s: %w", src, digestErr))
			}
			digestRef, digestParseErr := name.NewDigest(fmt.Sprintf("%s@%s", tagRef.Context().Name(), sourceDesc.Digest.String()), name.WeakValidation)
			if digestParseErr != nil {
				cancelSource()
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("build digest reference %s: %w", src, digestParseErr))
			}
			log.V(1).Info("resolved tag digest", "digest", sourceDesc.Digest.String())
			cancelSource()
			pullRef = digestRef
		}
	}

	desc, descCancel, err = getDescriptor(pullRef)
	if err != nil {
		logRegistryAuthError(log, err, "pull descriptor")
		metrics.RecordPullError(src)
		return p.failureResult(target, fmt.Errorf("describe %s: %w", src, err))
	}
	defer descCancel()

	log.V(1).Info("starting pull from source")
	log.V(1).Info("pull progress update", "percentage", "0%")

	if p.dryPull {
		log.Info(
			"dry pull: skipping source registry fetch",
			"result", "skipped",
			"dryPull", true,
			"sourceReference", pullRef.String(),
		)
		return nil
	}

	multiArch := desc.MediaType.IsIndex() && !p.pullByDigest

	var (
		img               v1.Image
		idx               v1.ImageIndex
		selectedFromIndex bool
	)

	if desc.MediaType.IsIndex() {
		if multiArch {
			idx, err = desc.ImageIndex()
			if err != nil {
				logRegistryAuthError(log, err, "pull")
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("load index %s: %w", src, err))
			}
			log.Info("Detected multi-architecture manifest lists", "mediaType", desc.MediaType, "action", "mirroring all manifests")
		} else {
			img, err = desc.Image()
			if err != nil {
				logRegistryAuthError(log, err, "pull")
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("pull %s: %w", src, err))
			}
			selectedFromIndex = true
		}
	} else {
		img, err = desc.Image()
		if err != nil {
			logRegistryAuthError(log, err, "pull")
			metrics.RecordPullError(src)
			return p.failureResult(target, fmt.Errorf("pull %s: %w", src, err))
		}
	}

	if selectedFromIndex {
		if cfg, cfgErr := img.ConfigFile(); cfgErr == nil && cfg != nil {
			log.Info(
				"Digest pull enabled; mirroring platform-specific manifest from index",
				"mediaType", desc.MediaType,
				"os", cfg.OS,
				"architecture", cfg.Architecture,
			)
		} else {
			log.Info(
				"Digest pull enabled; mirroring platform-specific manifest from index",
				"mediaType", desc.MediaType,
			)
		}
	}

	metrics.RecordPullSuccess(src)

	log.V(1).Info("finished pulling image from source")
	log.V(1).Info("pull progress update", "percentage", "100%")

	var srcDigest v1.Hash
	if multiArch {
		srcDigest, err = idx.Digest()
	} else {
		srcDigest, err = img.Digest()
	}
	if err != nil {
		metrics.RecordPullError(src)
		return p.failureResult(target, fmt.Errorf("digest %s: %w", src, err))
	}

	if selectedFromIndex && desc.Digest != (v1.Hash{}) && desc.Digest != srcDigest {
		log.V(1).Info(
			"resolved platform-specific digest from multi-architecture index",
			"indexDigest", desc.Digest.String(),
			"selectedDigest", srcDigest.String(),
		)
	}

	log.V(1).Info("resolving target registry credentials")

	username, password, err := p.target.BasicAuth(ctx)
	if err != nil {
		metrics.RecordPushError(target)
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
	headDesc, headErr := remote.Head(targetRef, remote.WithAuth(auth), remote.WithContext(headCtx), remote.WithTransport(transport(p.target.Insecure())))
	cancelHead()
	if headErr == nil {
		if headDesc.Digest == srcDigest {
			if p.dryRun {
				log.V(1).Info("image already present at target", "digest", srcDigest.String(), "dryRun", true, "result", "skipped")
			} else {
				log.V(1).Info("image already present at target", "digest", srcDigest.String())
			}
			return nil
		}

		switch targetTag := targetRef.(type) {
		case name.Tag:
			tagStr := targetTag.TagStr()
			if strings.EqualFold(tagStr, "latest") {
				log.V(1).Info("image already present with different digest for latest tag, updating", "currentDigest", headDesc.Digest.String(), "sourceDigest", srcDigest.String())
			} else if !p.allowDifferentDigestRepush {
				err := fmt.Errorf("target image %s exists with digest %s, refusing to overwrite with source digest %s", target, headDesc.Digest.String(), srcDigest.String())
				log.Error(err, "digest mismatch detected")
				metrics.RecordPushError(target)
				return p.failureResult(target, err)
			} else {
				log.V(1).Info("image already present with different digest, updating per configuration", "currentDigest", headDesc.Digest.String(), "sourceDigest", srcDigest.String())
			}
		default:
			log.V(1).Info("image already present with different digest, updating", "currentDigest", headDesc.Digest.String(), "sourceDigest", srcDigest.String())
		}
	} else if te, ok := headErr.(*remotetransport.Error); ok && te.StatusCode == http.StatusNotFound {
		// continue to push
	} else if headErr != nil {
		logRegistryAuthError(log, headErr, "target existence check")
		metrics.RecordPushError(target)
		return p.failureResult(target, fmt.Errorf("check %s: %w", target, headErr))
	}

	if err := p.target.EnsureRepository(ctx, repo); err != nil {
		metrics.RecordPushError(target)
		return p.failureResult(target, fmt.Errorf("ensure repo %s: %w", repo, err))
	}

	if p.dryRun {
		log.Info("dry run: skipping push", "result", "skipped", "dryRun", true)
		return nil
	}
	log.Info("pushing image to target", "digest", srcDigest.String())
	log.V(1).Info("push progress update", "percentage", "0%")

	pushCtx, cancelPush := p.operationContext(ctx)
	updates := make(chan v1.Update, 16)
	var progressWG sync.WaitGroup
	progressWG.Add(1)
	go func() {
		defer progressWG.Done()
		logProgressUpdates(log, "push", updates)
	}()

	if multiArch {
		err = remote.WriteIndex(
			targetRef,
			idx,
			remote.WithAuth(auth),
			remote.WithContext(pushCtx),
			remote.WithTransport(transport(p.target.Insecure())),
			remote.WithProgress(updates),
		)
	} else {
		err = remote.Write(
			targetRef,
			img,
			remote.WithAuth(auth),
			remote.WithContext(pushCtx),
			remote.WithTransport(transport(p.target.Insecure())),
			remote.WithProgress(updates),
		)
	}
	cancelPush()
	progressWG.Wait()
	if err != nil {
		logRegistryAuthError(log, err, "push")
		metrics.RecordPushError(target)
		return p.failureResult(target, fmt.Errorf("push %s: %w", target, err))
	}

	targetDigest := srcDigest
	verifyCtx, cancelVerify := p.operationContext(ctx)
	verifyDesc, verifyErr := remote.Head(
		targetRef,
		remote.WithAuth(auth),
		remote.WithContext(verifyCtx),
		remote.WithTransport(transport(p.target.Insecure())),
	)
	cancelVerify()
	switch {
	case verifyErr == nil:
		targetDigest = verifyDesc.Digest
		if targetDigest == srcDigest {
			log.Info("finished pushing image", "digest", targetDigest.String())
		} else {
			log.Info(
				"finished pushing image with different digest at target",
				"sourceDigest", srcDigest.String(),
				"targetDigest", targetDigest.String(),
			)
		}
	case errors.Is(verifyErr, context.DeadlineExceeded):
		log.V(1).Info("unable to confirm target digest after push", "reason", "timed out")
		log.Info("finished pushing image", "digest", targetDigest.String())
	case verifyErr != nil:
		log.V(1).Info("unable to confirm target digest after push", "error", verifyErr)
		log.Info("finished pushing image", "digest", targetDigest.String())
	}

	metrics.RecordPushSuccess(target)
	return nil
}

func logProgressUpdates(log logr.Logger, operation string, updates <-chan v1.Update) {
	const step = 10.0

	nextThreshold := step
	loggedFinal := false
	failed := false

	for update := range updates {
		if update.Error != nil {
			failed = true
			log.Error(update.Error, fmt.Sprintf("%s progress error", operation))
			continue
		}

		if update.Total <= 0 {
			continue
		}

		percent := (float64(update.Complete) / float64(update.Total)) * 100
		for percent >= nextThreshold && nextThreshold < 100 {
			log.V(1).Info(
				fmt.Sprintf("%s progress update", operation),
				"percentage", fmt.Sprintf("%.0f%%", nextThreshold),
				"completeBytes", update.Complete,
				"totalBytes", update.Total,
			)
			nextThreshold += step
		}

		if percent >= 100 && !loggedFinal {
			log.V(1).Info(
				fmt.Sprintf("%s progress update", operation),
				"percentage", "100%",
				"completeBytes", update.Complete,
				"totalBytes", update.Total,
			)
			loggedFinal = true
		}
	}

	if !failed && !loggedFinal {
		log.V(1).Info(
			fmt.Sprintf("%s progress update", operation),
			"percentage", "100%",
		)
	}
}

type registryAuthError struct {
	statusCode  int
	diagnostics []string
}

func logRegistryAuthError(log logr.Logger, err error, phase string) {
	if info, ok := detectRegistryAuthError(err); ok {
		msg := fmt.Sprintf("authentication to target registry failed during %s", phase)
		fields := []any{"statusCode", info.statusCode}
		if len(info.diagnostics) > 0 {
			fields = append(fields, "details", info.diagnostics)
		}
		log.Error(err, msg, fields...)
	}
}

func detectRegistryAuthError(err error) (*registryAuthError, bool) {
	var transportErr *remotetransport.Error
	if !errors.As(err, &transportErr) {
		return nil, false
	}

	if !isRegistryAuthStatus(transportErr.StatusCode) && !hasRegistryAuthDiagnostic(transportErr.Errors) {
		return nil, false
	}

	diagnostics := make([]string, 0, len(transportErr.Errors))
	for _, diag := range transportErr.Errors {
		diagnostics = append(diagnostics, diag.String())
	}

	return &registryAuthError{statusCode: transportErr.StatusCode, diagnostics: diagnostics}, true
}

func isRegistryAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

func hasRegistryAuthDiagnostic(diags []remotetransport.Diagnostic) bool {
	for _, diag := range diags {
		if diag.Code == remotetransport.UnauthorizedErrorCode || diag.Code == remotetransport.DeniedErrorCode {
			return true
		}
	}
	return false
}

func (p *pusher) DryRun() bool {
	return p.dryRun
}

func (p *pusher) DryPull() bool {
	return p.dryPull
}

func (p *pusher) ResetCooldown() (int, bool) {
	if p.failureCooldown <= 0 {
		return 0, false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	cleared := len(p.failed)
	if cleared == 0 {
		return 0, true
	}

	for target := range p.failed {
		delete(p.failed, target)
	}

	return cleared, true
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

func normalizeExcludedRegistries(provided []string) []string {
	unique := make(map[string]struct{})
	for _, val := range provided {
		normalized := normalizeRegistryPrefix(val)
		if normalized == "" {
			continue
		}
		unique[normalized] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}
	out := make([]string, 0, len(unique))
	for val := range unique {
		out = append(out, val)
	}
	sort.Strings(out)
	return out
}

func normalizeRegistryPrefix(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ToLower(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	return strings.TrimSuffix(trimmed, "/")
}

func normalizeImageReference(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ToLower(trimmed)
	trimmed = strings.TrimPrefix(trimmed, "https://")
	return strings.TrimPrefix(trimmed, "http://")
}

func hasBoundaryPrefix(s, prefix string) bool {
	if prefix == "" {
		return false
	}
	if !strings.HasPrefix(s, prefix) {
		return false
	}
	if len(s) == len(prefix) {
		return true
	}
	next := s[len(prefix)]
	return next == '/' || next == ':' || next == '@'
}

func (p *pusher) matchExcludedRegistry(src string) (string, bool) {
	if len(p.excludedRegistries) == 0 {
		return "", false
	}
	normalized := normalizeImageReference(src)
	if normalized == "" {
		return "", false
	}
	for _, prefix := range p.excludedRegistries {
		if hasBoundaryPrefix(normalized, prefix) {
			return prefix, true
		}
	}
	return "", false
}
