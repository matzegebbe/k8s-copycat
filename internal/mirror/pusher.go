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
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/matzegebbe/k8s-copycat/internal/registry"
	"github.com/matzegebbe/k8s-copycat/pkg/metrics"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	remoteGetFunc        = remote.Get
	remoteHeadFunc       = remote.Head
	remoteWriteFunc      = remote.Write
	remoteWriteIndexFunc = remote.WriteIndex
	remoteImageFunc      = remote.Image
	remoteIndexFunc      = remote.Index
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
	Architecture  string
	OS            string
	ImageID       string
}

type platformSpec struct {
	Architecture string
	OS           string
}

func (p platformSpec) key() string {
	arch := strings.ToLower(strings.TrimSpace(p.Architecture))
	os := strings.ToLower(strings.TrimSpace(p.OS))
	return os + "/" + arch
}

func (p platformSpec) toPlatform() *v1.Platform {
	arch := strings.TrimSpace(p.Architecture)
	os := strings.TrimSpace(p.OS)
	if arch == "" {
		return nil
	}
	if os == "" {
		os = "linux"
	}
	return &v1.Platform{Architecture: arch, OS: os}
}

func (p platformSpec) String() string {
	arch := strings.TrimSpace(p.Architecture)
	os := strings.TrimSpace(p.OS)
	if os == "" {
		return arch
	}
	if arch == "" {
		return os
	}
	return os + "/" + arch
}

func (p platformSpec) matches(desc *v1.Descriptor) bool {
	if desc == nil || desc.Platform == nil {
		return false
	}
	arch := strings.TrimSpace(desc.Platform.Architecture)
	os := strings.TrimSpace(desc.Platform.OS)
	if arch == "" || os == "" {
		return false
	}
	desiredArch := strings.TrimSpace(p.Architecture)
	desiredOS := strings.TrimSpace(p.OS)
	if desiredArch != "" && !strings.EqualFold(desiredArch, arch) {
		return false
	}
	if desiredOS != "" && !strings.EqualFold(desiredOS, os) {
		return false
	}
	return true
}

func normalizeImageID(imageID string) string {
	trimmed := strings.TrimSpace(imageID)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		trimmed = trimmed[idx+3:]
	}
	return strings.TrimSpace(trimmed)
}

func parsePlatformSpec(value string) (platformSpec, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return platformSpec{}, fmt.Errorf("empty platform")
	}
	var spec platformSpec
	if strings.Contains(trimmed, "/") {
		parts := strings.SplitN(trimmed, "/", 2)
		spec.OS = strings.TrimSpace(parts[0])
		spec.Architecture = strings.TrimSpace(parts[1])
	} else {
		spec.OS = "linux"
		spec.Architecture = trimmed
	}
	if spec.Architecture == "" {
		return platformSpec{}, fmt.Errorf("missing architecture in platform %q", value)
	}
	if spec.OS == "" {
		spec.OS = "linux"
	}
	return spec, nil
}

func parseMirrorPlatforms(logger logr.Logger, values []string) ([]platformSpec, map[string]struct{}) {
	if len(values) == 0 {
		return nil, nil
	}
	parsed := make([]platformSpec, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		spec, err := parsePlatformSpec(raw)
		if err != nil {
			logger.Info("ignoring invalid mirror platform", "value", strings.TrimSpace(raw), "error", err.Error())
			continue
		}
		key := spec.key()
		if _, ok := seen[key]; ok {
			continue
		}
		parsed = append(parsed, spec)
		seen[key] = struct{}{}
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	return parsed, seen
}

func digestReferenceFromImageID(imageID string, src name.Reference) (string, name.Digest, error) {
	normalized := normalizeImageID(imageID)
	if normalized == "" {
		return "", name.Digest{}, fmt.Errorf("empty imageID")
	}
	if strings.Contains(normalized, "@") {
		ref, err := name.NewDigest(normalized, name.WeakValidation)
		if err != nil {
			return "", name.Digest{}, err
		}
		return ref.DigestStr(), ref, nil
	}
	if _, err := v1.NewHash(normalized); err != nil {
		return "", name.Digest{}, err
	}
	contextName := src.Context().Name()
	ref, err := name.NewDigest(fmt.Sprintf("%s@%s", contextName, normalized), name.WeakValidation)
	if err != nil {
		return "", name.Digest{}, err
	}
	return normalized, ref, nil
}

func pullReferenceFromMetadata(pullByDigest bool, normalizedImageID string, src name.Reference) (name.Reference, string, bool, error) {
	if !pullByDigest {
		return src, "", false, nil
	}

	if normalizedImageID == "" {
		return src, "", false, nil
	}

	digestStr, digestRef, err := digestReferenceFromImageID(normalizedImageID, src)
	if err != nil {
		return src, "", false, err
	}

	return digestRef, digestStr, true, nil
}

type pusher struct {
	target                     registry.Target
	dryRun                     bool
	dryPull                    bool
	transform                  func(string) string
	pullByDigest               bool
	allowDifferentDigestRepush bool
	mirrorPlatforms            []platformSpec
	mirrorPlatformSet          map[string]struct{}
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

func NewPusher(t registry.Target, dryRun bool, dryPull bool, transform func(string) string, logger logr.Logger, keychain authn.Keychain, requestTimeout time.Duration, failureCooldown time.Duration, pullByDigest bool, allowDifferentDigestRepush bool, excluded []string, mirrorPlatforms []string) Pusher {
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
	parsedPlatforms, platformSet := parseMirrorPlatforms(logger, mirrorPlatforms)

	return &pusher{
		target:                     t,
		dryRun:                     dryRun,
		dryPull:                    dryPull,
		transform:                  transform,
		pullByDigest:               pullByDigest,
		allowDifferentDigestRepush: allowDifferentDigestRepush,
		mirrorPlatforms:            parsedPlatforms,
		mirrorPlatformSet:          platformSet,
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
	baseLog := log.WithValues(
		"source", src,
		"namespace", meta.Namespace,
	)
	if meta.PodName != "" {
		baseLog = baseLog.WithValues("pod", meta.PodName)
	}
	if meta.ContainerName != "" {
		baseLog = baseLog.WithValues("container", meta.ContainerName)
	}
	log = baseLog

	if excluded, ok := p.matchExcludedRegistry(src); ok {
		log.V(1).Info(
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
	var podDigestStr string
	havePodDigest := false
	opts := []name.Option{name.WeakValidation}
	if p.target.Insecure() {
		opts = append(opts, name.Insecure)
	}
	buildTarget := func(repo string) (string, name.Reference, error) {
		switch r := srcRef.(type) {
		case name.Tag:
			ref := fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, r.TagStr())
			tgt, tgtErr := name.NewTag(ref, opts...)
			return ref, tgt, tgtErr
		case name.Digest:
			stripped := src
			if idx := strings.Index(stripped, "@"); idx > 0 {
				stripped = stripped[:idx]
			}
			// Try to honour the original tag when the source reference included both tag and digest.
			if tagRef, tagErr := name.NewTag(stripped, name.WeakValidation); tagErr == nil {
				ref := fmt.Sprintf("%s/%s:%s", p.target.Registry(), repo, tagRef.TagStr())
				tgt, tgtErr := name.NewTag(ref, opts...)
				return ref, tgt, tgtErr
			}
			ref := fmt.Sprintf("%s/%s@%s", p.target.Registry(), repo, r.DigestStr())
			tgt, tgtErr := name.NewDigest(ref, opts...)
			return ref, tgt, tgtErr
		default:
			return "", nil, fmt.Errorf("unsupported reference type %T", srcRef)
		}
	}

	target, targetRef, err = buildTarget(repo)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}

	log = baseLog.WithValues("target", target)
	procLog := log

	procLog.V(1).Info("resolved target reference", "reference", target)

	sourceIsDigest := false
	if _, ok := srcRef.(name.Digest); ok {
		sourceIsDigest = true
	}

	normalizedID := normalizeImageID(meta.ImageID)
	pullRef, podDigestStr, havePodDigest, pullRefErr := pullReferenceFromMetadata(p.pullByDigest, normalizedID, srcRef)
	if pullRefErr != nil {
		log.V(1).Error(pullRefErr, "failed to parse digest from pod imageID", "imageID", normalizedID)
	} else if havePodDigest {
		log.V(1).Info("using pod imageID digest for pull", "imageID", normalizedID)
	}

	if p.pullByDigest && !havePodDigest && !sourceIsDigest {
		procLog.V(1).Info(
			"digest pull enabled but pod imageID digest is not available yet, skipping until it is reported",
			"result", "skipped",
		)
		return nil
	}

	if skip, err := p.beginProcessing(target, procLog); err != nil {
		procLog.Error(err, "unable to begin processing")
		return err
	} else if skip {
		return nil
	}

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

	if p.pullByDigest && havePodDigest {
		var digestRef name.Reference
		if existingDigest, ok := targetRef.(name.Digest); ok && strings.EqualFold(existingDigest.DigestStr(), podDigestStr) {
			digestRef = existingDigest
		} else {
			digestName := fmt.Sprintf("%s@%s", targetRef.Context().Name(), podDigestStr)
			targetDigestRef, digestErr := name.NewDigest(digestName, opts...)
			if digestErr != nil {
				log.V(1).Error(digestErr, "unable to build target digest reference", "digest", podDigestStr)
			} else {
				digestRef = targetDigestRef
			}
		}

		if digestRef != nil {
			headCtx, cancelHead := p.operationContext(ctx)
			_, headErr := remoteHeadFunc(digestRef, remote.WithAuth(auth), remote.WithContext(headCtx), remote.WithTransport(transport(p.target.Insecure())))
			cancelHead()
			if headErr == nil {
				log.V(1).Info("image digest already present at target", "digest", podDigestStr, "result", "skipped")
				return nil
			}
			if te, ok := headErr.(*remotetransport.Error); ok && te.StatusCode == http.StatusNotFound {
				// continue to pull and push
			} else if headErr != nil {
				log.V(1).Error(headErr, "unable to confirm existing digest", "digest", podDigestStr)
			}
		}
	}

	getDescriptor := func(ref name.Reference, platform *v1.Platform) (*remote.Descriptor, context.CancelFunc, error) {
		descCtx, cancel := p.operationContext(ctx)
		opts := []remote.Option{
			remote.WithContext(descCtx),
			remote.WithAuthFromKeychain(p.keychain),
			remote.WithTransport(transport(p.target.Insecure())),
		}
		if platform != nil {
			opts = append(opts, remote.WithPlatform(*platform))
		}
		desc, err := remoteGetFunc(ref, opts...)
		if err != nil {
			cancel()
			return nil, func() {}, err
		}
		return desc, cancel, nil
	}

	var desc *remote.Descriptor
	descCancel := func() {}

	metaPlatform := platformFromMetadata(meta)
	desiredPlatforms := p.desiredPlatforms(metaPlatform)
	primaryPlatform := metaPlatform
	if len(desiredPlatforms) > 0 {
		primaryPlatform = desiredPlatforms[0].toPlatform()
	}

	requestPlatform := primaryPlatform
	if len(desiredPlatforms) > 1 {
		requestPlatform = nil
	}

	if spec, ok := specFromPlatform(metaPlatform); ok && len(p.mirrorPlatformSet) > 0 {
		if _, allowed := p.mirrorPlatformSet[spec.key()]; !allowed {
			log.WithValues("severity", "warning").Info(
				"checkNodePlatform detected platform not configured in mirrorPlatforms; continuing with node-specific manifest",
				"architecture", metaPlatform.Architecture,
				"os", metaPlatform.OS,
			)
		}
	}

	desc, descCancel, err = getDescriptor(pullRef, requestPlatform)
	if err != nil {
		logRegistryAuthError(log, err, "pull descriptor")
		metrics.RecordPullError(src)
		return p.failureResult(target, fmt.Errorf("describe %s: %w", src, err))
	}
	defer descCancel()

	log.V(1).Info("starting pull from source")
	log.V(1).Info("pull progress update", "percentage", "0%")

	if p.dryPull {
		log.V(1).Info(
			"dry pull: skipping source registry fetch",
			"result", "skipped",
			"dryPull", true,
			"sourceReference", pullRef.String(),
		)
		return nil
	}

	pushIndex := false

	var (
		img               v1.Image
		idx               v1.ImageIndex
		selectedFromIndex bool
	)

	if len(p.mirrorPlatforms) > 0 && len(desiredPlatforms) > 1 && !desc.MediaType.IsIndex() {
		logUnavailablePlatforms(log, src, desiredPlatforms[1:])
	}

	switch {
	case desc.MediaType.IsIndex() && p.pullByDigest && len(desiredPlatforms) > 1:
		idx, err = desc.ImageIndex()
		if err != nil {
			logRegistryAuthError(log, err, "pull")
			metrics.RecordPullError(src)
			return p.failureResult(target, fmt.Errorf("load index %s: %w", src, err))
		}
		filtered, matched, missing, filterErr := p.filterIndexByPlatforms(ctx, log, idx, desiredPlatforms, targetRef.Context(), auth, opts)
		if filterErr != nil {
			logRegistryAuthError(log, filterErr, "pull")
			metrics.RecordPullError(src)
			return p.failureResult(target, fmt.Errorf("filter index %s: %w", src, filterErr))
		}
		if len(matched) == 0 {
			log.Info(
				"configured mirrorPlatforms not found in source index; mirroring full index",
				"requestedPlatforms", specsToStrings(desiredPlatforms),
			)
		} else {
			idx = filtered
			if len(missing) > 0 {
				logUnavailablePlatforms(log, src, missing)
				log.Info(
					"some configured mirrorPlatforms missing from source index", "missingPlatforms", specsToStrings(missing),
				)
			}
			log.V(1).Info(
				"mirroring configured subset of multi-architecture index",
				"platforms", specsToStrings(matched),
			)
		}
		pushIndex = true
	case shouldMirrorEntireIndex(desc.MediaType, p.pullByDigest, primaryPlatform):
		idx, err = desc.ImageIndex()
		if err != nil {
			logRegistryAuthError(log, err, "pull")
			metrics.RecordPullError(src)
			return p.failureResult(target, fmt.Errorf("load index %s: %w", src, err))
		}
		pushIndex = true
		if primaryPlatform == nil {
			log.V(1).Info("mirroring entire multi-architecture index", "mediaType", desc.MediaType, "reason", "platform metadata unavailable")
		} else {
			log.V(1).Info("mirroring entire multi-architecture index", "mediaType", desc.MediaType, "reason", "digestPull disabled")
		}
	default:
		img, err = desc.Image()
		if err != nil {
			if desc.MediaType.IsIndex() {
				idx, idxErr := desc.ImageIndex()
				if idxErr != nil {
					logRegistryAuthError(log, idxErr, "pull")
					metrics.RecordPullError(src)
					return p.failureResult(target, fmt.Errorf("load index %s: %w", src, idxErr))
				}
				var selectErr error
				img, selectErr = imageFromIndex(idx, primaryPlatform)
				if selectErr != nil {
					logRegistryAuthError(log, selectErr, "pull")
					metrics.RecordPullError(src)
					return p.failureResult(target, fmt.Errorf("resolve platform image %s: %w", src, selectErr))
				}
				selectedFromIndex = true
			} else {
				logRegistryAuthError(log, err, "pull")
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("pull %s: %w", src, err))
			}
		} else if desc.MediaType.IsIndex() {
			selectedFromIndex = true
		}
	}

	if selectedFromIndex {
		if cfg, cfgErr := img.ConfigFile(); cfgErr == nil && cfg != nil {
			log.V(1).Info(
				"Digest pull enabled; mirroring platform-specific manifest from index",
				"mediaType", desc.MediaType,
				"os", cfg.OS,
				"architecture", cfg.Architecture,
			)
		} else {
			log.V(1).Info(
				"Digest pull enabled; mirroring platform-specific manifest from index",
				"mediaType", desc.MediaType,
			)
		}
	}

	metrics.RecordPullSuccess(src)

	log.V(1).Info("finished pulling image from source")
	log.V(1).Info("pull progress update", "percentage", "100%")

	if arch := resolveArchitecture(pushIndex, idx, img); arch != "" {
		meta.Architecture = arch
		newRepo := p.resolveRepoPath(srcRepo, meta)
		if newRepo != repo {
			newTarget, newTargetRef, buildErr := buildTarget(newRepo)
			if buildErr != nil {
				metrics.RecordPullError(src)
				return p.failureResult(target, fmt.Errorf("parse target %s: %w", newRepo, buildErr))
			}

			reassignedLog := baseLog.WithValues("target", newTarget)
			skip, reassignErr := p.reassignProcessing(target, newTarget, reassignedLog)
			if reassignErr != nil {
				return reassignErr
			}
			if skip {
				return nil
			}

			repo = newRepo
			target = newTarget
			targetRef = newTargetRef
			log = reassignedLog
		}
	}

	var srcDigest v1.Hash
	if pushIndex {
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

	// Skip if image already exists in target registry with the same digest.
	headCtx, cancelHead := p.operationContext(ctx)
	headDesc, headErr := remoteHeadFunc(targetRef, remote.WithAuth(auth), remote.WithContext(headCtx), remote.WithTransport(transport(p.target.Insecure())))
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
		log.V(1).Info("dry run: skipping push", "result", "skipped", "dryRun", true)
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

	if pushIndex {
		err = remoteWriteIndexFunc(
			targetRef,
			idx,
			remote.WithAuth(auth),
			remote.WithContext(pushCtx),
			remote.WithTransport(transport(p.target.Insecure())),
			remote.WithProgress(updates),
		)
	} else {
		err = remoteWriteFunc(
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
	verifyDesc, verifyErr := remoteHeadFunc(
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
			log.V(1).Info("image already processed during current run", "result", "skipped", "dryRun", true)
		} else {
			log.V(1).Info("image already processed during current run", "result", "skipped")
		}
		return true, nil
	}

	p.pushed[target] = struct{}{}
	return false, nil
}

func (p *pusher) reassignProcessing(oldTarget, newTarget string, log logr.Logger) (bool, error) {
	if oldTarget == newTarget {
		return false, nil
	}

	log.V(1).Info("updating resolved target", "previous", oldTarget)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failureCooldown > 0 {
		if lastFailure, ok := p.failed[newTarget]; ok {
			retryAt := lastFailure.Add(p.failureCooldown)
			now := p.now()
			if now.Before(retryAt) {
				delete(p.pushed, oldTarget)
				err := &RetryError{Cause: ErrInCooldown, RetryAt: retryAt}
				log.Error(err, "skipping image due to previous failure", "retryAt", retryAt)
				return false, err
			}
			delete(p.failed, newTarget)
		}
		if lastFailure, ok := p.failed[oldTarget]; ok {
			p.failed[newTarget] = lastFailure
			delete(p.failed, oldTarget)
		}
	}

	if _, exists := p.pushed[newTarget]; exists {
		delete(p.pushed, oldTarget)
		if p.dryRun {
			log.V(1).Info("image already processed during current run", "result", "skipped", "dryRun", true)
		} else {
			log.V(1).Info("image already processed during current run", "result", "skipped")
		}
		return true, nil
	}

	if _, exists := p.pushed[oldTarget]; exists {
		delete(p.pushed, oldTarget)
		p.pushed[newTarget] = struct{}{}
	}

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

func platformFromMetadata(meta Metadata) *v1.Platform {
	arch := strings.TrimSpace(meta.Architecture)
	os := strings.TrimSpace(meta.OS)
	if arch == "" && os == "" {
		return nil
	}
	if arch == "" {
		return nil
	}
	if os == "" {
		os = "linux"
	}
	return &v1.Platform{Architecture: arch, OS: os}
}

func imageFromIndex(idx v1.ImageIndex, platform *v1.Platform) (v1.Image, error) {
	if idx == nil {
		return nil, fmt.Errorf("image index is nil")
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, err
	}

	selected, err := selectImageDescriptor(manifest, platform)
	if err != nil {
		return nil, err
	}

	return idx.Image(selected.Digest)
}

func selectImageDescriptor(manifest *v1.IndexManifest, platform *v1.Platform) (*v1.Descriptor, error) {
	if manifest == nil || len(manifest.Manifests) == 0 {
		return nil, fmt.Errorf("image index has no manifests")
	}

	runnable := make([]*v1.Descriptor, 0, len(manifest.Manifests))
	for i := range manifest.Manifests {
		candidate := &manifest.Manifests[i]
		if descriptorIsRunnable(candidate) {
			runnable = append(runnable, candidate)
		}
	}
	if len(runnable) == 0 {
		return nil, fmt.Errorf("image index has no runnable manifests")
	}

	if platform != nil {
		desiredArch := strings.TrimSpace(platform.Architecture)
		desiredOS := strings.TrimSpace(platform.OS)
		for _, candidate := range runnable {
			arch := strings.TrimSpace(candidate.Platform.Architecture)
			os := strings.TrimSpace(candidate.Platform.OS)
			if desiredArch != "" && !strings.EqualFold(arch, desiredArch) {
				continue
			}
			if desiredOS != "" && !strings.EqualFold(os, desiredOS) {
				continue
			}
			return candidate, nil
		}
	}

	return runnable[0], nil
}

func descriptorIsRunnable(desc *v1.Descriptor) bool {
	if desc == nil {
		return false
	}
	if typ, ok := desc.Annotations["vnd.docker.reference.type"]; ok {
		if strings.EqualFold(strings.TrimSpace(typ), "attestation-manifest") {
			return false
		}
	}
	if desc.Platform == nil {
		return false
	}
	arch := strings.TrimSpace(desc.Platform.Architecture)
	if arch == "" || strings.EqualFold(arch, "unknown") {
		return false
	}
	os := strings.TrimSpace(desc.Platform.OS)
	if os == "" || strings.EqualFold(os, "unknown") {
		return false
	}
	return true
}

func specFromPlatform(platform *v1.Platform) (platformSpec, bool) {
	if platform == nil {
		return platformSpec{}, false
	}
	arch := strings.TrimSpace(platform.Architecture)
	if arch == "" {
		return platformSpec{}, false
	}
	os := strings.TrimSpace(platform.OS)
	if os == "" {
		os = "linux"
	}
	return platformSpec{Architecture: arch, OS: os}, true
}

func (p *pusher) desiredPlatforms(metaPlatform *v1.Platform) []platformSpec {
	desired := make([]platformSpec, 0, len(p.mirrorPlatforms)+1)
	seen := make(map[string]struct{}, len(p.mirrorPlatforms)+1)
	if spec, ok := specFromPlatform(metaPlatform); ok {
		key := spec.key()
		desired = append(desired, spec)
		seen[key] = struct{}{}
	}
	for _, spec := range p.mirrorPlatforms {
		key := spec.key()
		if _, ok := seen[key]; ok {
			continue
		}
		desired = append(desired, spec)
		seen[key] = struct{}{}
	}
	return desired
}

func (p *pusher) filterIndexByPlatforms(
	ctx context.Context,
	logger logr.Logger,
	idx v1.ImageIndex,
	desired []platformSpec,
	targetRepo name.Repository,
	auth authn.Authenticator,
	nameOpts []name.Option,
) (v1.ImageIndex, []platformSpec, []platformSpec, error) {
	manifest, err := idx.IndexManifest()
	if err != nil {
		return nil, nil, nil, err
	}
	if manifest == nil {
		return nil, nil, desired, fmt.Errorf("image index has no manifests")
	}
	adds := make([]mutate.IndexAddendum, 0, len(desired))
	matched := make([]platformSpec, 0, len(desired))
	missing := make([]platformSpec, 0, len(desired))
	for _, spec := range desired {
		desc := findDescriptorForSpec(manifest, spec)
		if desc == nil {
			missing = append(missing, spec)
			continue
		}
		appendable, appendErr := p.appendableForFilteredDescriptor(ctx, logger, idx, desc, spec, targetRepo, auth, nameOpts)
		if appendErr != nil {
			return nil, nil, nil, appendErr
		}
		adds = append(adds, mutate.IndexAddendum{Add: appendable, Descriptor: *desc})
		matched = append(matched, spec)
	}
	if len(adds) == 0 {
		return idx, matched, missing, nil
	}
	filtered := mutate.AppendManifests(empty.Index, adds...)
	return filtered, matched, missing, nil
}

func appendableForDescriptor(idx v1.ImageIndex, desc *v1.Descriptor) (mutate.Appendable, error) {
	if desc == nil {
		return nil, fmt.Errorf("descriptor is nil")
	}
	if desc.MediaType.IsIndex() {
		return idx.ImageIndex(desc.Digest)
	}
	return idx.Image(desc.Digest)
}

func (p *pusher) appendableForFilteredDescriptor(
	ctx context.Context,
	logger logr.Logger,
	idx v1.ImageIndex,
	desc *v1.Descriptor,
	spec platformSpec,
	targetRepo name.Repository,
	auth authn.Authenticator,
	nameOpts []name.Option,
) (mutate.Appendable, error) {
	if desc == nil {
		return nil, fmt.Errorf("descriptor is nil")
	}

	if targetRepo != (name.Repository{}) {
		digestName := fmt.Sprintf("%s@%s", targetRepo.Name(), desc.Digest.String())
		targetDigestRef, err := name.NewDigest(digestName, nameOpts...)
		if err != nil {
			return nil, err
		}

		headCtx, cancelHead := p.operationContext(ctx)
		_, headErr := remoteHeadFunc(targetDigestRef, remote.WithAuth(auth), remote.WithContext(headCtx), remote.WithTransport(transport(p.target.Insecure())))
		cancelHead()

		if headErr == nil {
			logger.V(1).Info(
				"platform-specific manifest already present at target",
				"platform", spec.String(),
				"digest", desc.Digest.String(),
			)

			fetchCtx, cancelFetch := p.operationContext(ctx)
			fetchOpts := []remote.Option{
				remote.WithAuth(auth),
				remote.WithContext(fetchCtx),
				remote.WithTransport(transport(p.target.Insecure())),
			}

			var appendable mutate.Appendable
			if desc.MediaType.IsIndex() {
				appendable, err = remoteIndexFunc(targetDigestRef, fetchOpts...)
			} else {
				appendable, err = remoteImageFunc(targetDigestRef, fetchOpts...)
			}
			cancelFetch()

			if err == nil {
				return appendable, nil
			}

			logger.V(1).Error(
				err,
				"unable to load platform manifest from target, falling back to source",
				"platform", spec.String(),
				"digest", desc.Digest.String(),
			)
		} else if te, ok := headErr.(*remotetransport.Error); ok {
			if te.StatusCode != http.StatusNotFound {
				logger.V(1).Error(headErr, "target platform manifest check failed", "platform", spec.String(), "digest", desc.Digest.String())
				return nil, fmt.Errorf("check platform %s: %w", spec.String(), headErr)
			}
		} else if headErr != nil {
			logger.V(1).Error(headErr, "target platform manifest check failed", "platform", spec.String(), "digest", desc.Digest.String())
			return nil, fmt.Errorf("check platform %s: %w", spec.String(), headErr)
		}
	}

	return appendableForDescriptor(idx, desc)
}

func findDescriptorForSpec(manifest *v1.IndexManifest, spec platformSpec) *v1.Descriptor {
	if manifest == nil {
		return nil
	}
	for i := range manifest.Manifests {
		candidate := &manifest.Manifests[i]
		if !descriptorIsRunnable(candidate) {
			continue
		}
		if spec.matches(candidate) {
			return candidate
		}
	}
	return nil
}

func specsToStrings(specs []platformSpec) []string {
	if len(specs) == 0 {
		return nil
	}
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.String())
	}
	return out
}

func logUnavailablePlatforms(logger logr.Logger, src string, specs []platformSpec) {
	if len(specs) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		platform := spec.String()
		if platform == "" {
			continue
		}
		if _, alreadyLogged := seen[platform]; alreadyLogged {
			continue
		}
		logger.Info(
			fmt.Sprintf("image %s does not offer platform %s", src, platform),
			"platform", platform,
		)
		seen[platform] = struct{}{}
	}
}

func shouldMirrorEntireIndex(mediaType types.MediaType, pullByDigest bool, platform *v1.Platform) bool {
	if !mediaType.IsIndex() {
		return false
	}
	if !pullByDigest {
		return true
	}
	return platform == nil
}

func resolveArchitecture(useIndex bool, idx v1.ImageIndex, img v1.Image) string {
	if useIndex {
		if idx != nil {
			if manifest, err := idx.IndexManifest(); err == nil {
				seen := make(map[string]struct{})
				for _, m := range manifest.Manifests {
					if m.Platform == nil {
						continue
					}
					arch := strings.TrimSpace(m.Platform.Architecture)
					if arch == "" || strings.EqualFold(arch, "unknown") {
						continue
					}
					seen[arch] = struct{}{}
				}
				if len(seen) > 0 {
					vals := make([]string, 0, len(seen))
					for arch := range seen {
						vals = append(vals, arch)
					}
					sort.Strings(vals)
					return strings.Join(vals, "-")
				}
			}
		}
		return "multiarch"
	}

	if img != nil {
		if cfg, err := img.ConfigFile(); err == nil && cfg != nil {
			arch := strings.TrimSpace(cfg.Architecture)
			if arch != "" {
				return arch
			}
		}
	}

	return ""
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
		"$arch", meta.Architecture,
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
