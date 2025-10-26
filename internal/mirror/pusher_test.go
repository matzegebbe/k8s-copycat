package mirror

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/go-logr/logr/testr"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	remotetransport "github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/matzegebbe/k8s-copycat/pkg/metrics"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type fakeTarget struct {
	prefix   string
	insecure bool
}

func (f fakeTarget) Registry() string                                    { return "example.com" }
func (f fakeTarget) RepoPrefix() string                                  { return f.prefix }
func (f fakeTarget) EnsureRepository(_ context.Context, _ string) error  { return nil }
func (f fakeTarget) BasicAuth(_ context.Context) (string, string, error) { return "", "", nil }
func (f fakeTarget) Insecure() bool                                      { return f.insecure }

type authErrorTarget struct {
	fakeTarget
	err error
}

func (t authErrorTarget) BasicAuth(_ context.Context) (string, string, error) {
	return "", "", t.err
}

func TestResolveRepoPathWithMetadata(t *testing.T) {
	p := &pusher{
		target:    fakeTarget{prefix: "$namespace/$podname/$container_name"},
		transform: util.CleanRepoName,
	}

	repo := p.resolveRepoPath("library/nginx", Metadata{Namespace: "team-a", PodName: "pod-1", ContainerName: "app"})
	want := "team-a/pod-1/app/library/nginx"
	if repo != want {
		t.Fatalf("expected %q, got %q", want, repo)
	}
}

func TestNormalizeImageIDStripsRuntimePrefixes(t *testing.T) {
	cases := map[string]string{
		"docker-pullable://docker.io/library/nginx@sha256:abc": "docker.io/library/nginx@sha256:abc",
		"containerd://sha256:abcdef":                           "sha256:abcdef",
		"  cri-o://quay.io/app@sha256:def  ":                   "quay.io/app@sha256:def",
		"nerdctl://registry.example.com/repo@sha256:123":       "registry.example.com/repo@sha256:123",
		"": "",
	}

	for input, want := range cases {
		if got := normalizeImageID(input); got != want {
			t.Fatalf("normalizeImageID(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestDigestReferenceFromImageIDUsesPodDigest(t *testing.T) {
	srcRef, err := name.ParseReference("docker.io/library/alpine:3.19", name.WeakValidation)
	if err != nil {
		t.Fatalf("unexpected error parsing source reference: %v", err)
	}

	digestStr, digestRef, err := digestReferenceFromImageID(
		"docker.io/library/alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		srcRef,
	)
	if err != nil {
		t.Fatalf("unexpected error resolving digest reference: %v", err)
	}
	if digestStr != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected digest string sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef, got %q", digestStr)
	}
	if digestRef.Context().Name() != "index.docker.io/library/alpine" {
		t.Fatalf("expected context index.docker.io/library/alpine, got %q", digestRef.Context().Name())
	}
	if digestRef.Identifier() != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("expected digest identifier sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef, got %q", digestRef.Identifier())
	}
}

func TestDigestReferenceFromImageIDInfersContextForBareDigests(t *testing.T) {
	srcRef, err := name.ParseReference("ghcr.io/org/app:1.2.3", name.WeakValidation)
	if err != nil {
		t.Fatalf("unexpected error parsing source reference: %v", err)
	}

	digestStr, digestRef, err := digestReferenceFromImageID(
		"sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000",
		srcRef,
	)
	if err != nil {
		t.Fatalf("unexpected error resolving digest reference: %v", err)
	}
	if digestStr != "sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000" {
		t.Fatalf("expected digest string sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000, got %q", digestStr)
	}
	if got := digestRef.Context().Name(); got != "ghcr.io/org/app" {
		t.Fatalf("expected context ghcr.io/org/app, got %q", got)
	}
	if digestRef.Identifier() != "sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000" {
		t.Fatalf("expected digest identifier sha256:111122223333444455556666777788889999aaaabbbbccccddddeeeeffff0000, got %q", digestRef.Identifier())
	}
}

func TestPullReferenceFromMetadataUsesPodDigest(t *testing.T) {
	srcRef, err := name.ParseReference("docker.io/library/alpine:3.19", name.WeakValidation)
	if err != nil {
		t.Fatalf("unexpected error parsing source reference: %v", err)
	}

	normalized := "docker.io/library/alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	pullRef, digestStr, haveDigest, helperErr := pullReferenceFromMetadata(true, normalized, srcRef)
	if helperErr != nil {
		t.Fatalf("unexpected helper error: %v", helperErr)
	}
	if !haveDigest {
		t.Fatalf("expected helper to report pod digest availability")
	}
	if digestStr != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected digest string %q", digestStr)
	}
	if pullRef.Identifier() != digestStr {
		t.Fatalf("expected pull reference to use digest, got %q", pullRef.Identifier())
	}
}

func TestPullReferenceFromMetadataFallsBackWhenDisabled(t *testing.T) {
	srcRef, err := name.ParseReference("docker.io/library/nginx:1.28", name.WeakValidation)
	if err != nil {
		t.Fatalf("unexpected error parsing source reference: %v", err)
	}

	pullRef, digestStr, haveDigest, helperErr := pullReferenceFromMetadata(false, "docker.io/library/nginx@sha256:abc", srcRef)
	if helperErr != nil {
		t.Fatalf("unexpected helper error: %v", helperErr)
	}
	if haveDigest {
		t.Fatalf("expected helper not to report digest when disabled")
	}
	if digestStr != "" {
		t.Fatalf("expected empty digest string when digest pull disabled")
	}
	if pullRef.String() != srcRef.String() {
		t.Fatalf("expected pull reference to remain unchanged, got %q", pullRef.String())
	}
}

func TestPullReferenceFromMetadataPropagatesErrors(t *testing.T) {
	srcRef, err := name.ParseReference("docker.io/library/nginx:1.28", name.WeakValidation)
	if err != nil {
		t.Fatalf("unexpected error parsing source reference: %v", err)
	}

	_, _, _, helperErr := pullReferenceFromMetadata(true, "not-a-digest", srcRef)
	if helperErr == nil {
		t.Fatalf("expected helper to propagate parsing error")
	}
}

func TestResolveRepoPathWithArchitectureMetadata(t *testing.T) {
	p := &pusher{
		target:    fakeTarget{prefix: "$arch/$namespace"},
		transform: util.CleanRepoName,
	}

	repo := p.resolveRepoPath("", Metadata{Namespace: "prod", Architecture: "arm64"})
	if repo != "arm64/prod/library/unknown" {
		t.Fatalf("expected architecture placeholder to be expanded, got %q", repo)
	}
}

func TestDryPullOption(t *testing.T) {
	p := NewPusher(fakeTarget{}, true, true, nil, testr.New(t), nil, 0, 0, false, true, nil)

	if !p.DryPull() {
		t.Fatalf("expected dry pull to be enabled")
	}
}

func TestNewPusherConfiguresExcludedRegistries(t *testing.T) {
	p := NewPusher(fakeTarget{}, false, false, nil, testr.New(t), nil, 0, 0, false, true, []string{"registry.gitlab.com/team/"})

	impl, ok := p.(*pusher)
	if !ok {
		t.Fatalf("expected *pusher, got %T", p)
	}

	want := []string{
		"registry.gitlab.com/team",
	}
	if !reflect.DeepEqual(impl.excludedRegistries, want) {
		t.Fatalf("unexpected excluded registries: %#v", impl.excludedRegistries)
	}
}

func TestMirrorRecordsPullErrorMetric(t *testing.T) {
	metrics.Reset()
	t.Cleanup(metrics.Reset)

	original := remoteGetFunc
	remoteGetFunc = func(name.Reference, ...remote.Option) (*remote.Descriptor, error) {
		return nil, errors.New("pull failed")
	}
	t.Cleanup(func() { remoteGetFunc = original })

	p := NewPusher(fakeTarget{}, false, false, nil, testr.New(t), nil, 0, 0, false, true, nil)
	ctx := context.Background()

	err := p.Mirror(ctx, "docker.io/library/nginx:latest", Metadata{})
	if err == nil {
		t.Fatalf("expected pull error from Mirror")
	}

	got := counterValue(t, metrics.PullErrorCounter().WithLabelValues("docker.io/library/nginx:latest"))
	if got != 1 {
		t.Fatalf("expected pull_error_total to increment once, got %v", got)
	}
}

func TestMirrorRecordsPushErrorMetric(t *testing.T) {
	metrics.Reset()
	t.Cleanup(metrics.Reset)

	p := NewPusher(authErrorTarget{fakeTarget: fakeTarget{}, err: errors.New("auth failed")}, false, false, nil, testr.New(t), nil, 0, 0, false, true, nil)
	ctx := context.Background()

	err := p.Mirror(ctx, "docker.io/library/nginx:1.25", Metadata{})
	if err == nil {
		t.Fatalf("expected push error from Mirror")
	}

	got := counterValue(t, metrics.PushErrorCounter().WithLabelValues("example.com/library/nginx:1.25"))
	if got != 1 {
		t.Fatalf("expected push_error_total to increment once, got %v", got)
	}
}

func counterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	metric := &dto.Metric{}
	if err := c.Write(metric); err != nil {
		t.Fatalf("failed to read counter: %v", err)
	}
	if metric.Counter == nil {
		t.Fatalf("expected counter metric")
	}
	return metric.Counter.GetValue()
}

func TestMatchExcludedRegistry(t *testing.T) {
	p := &pusher{excludedRegistries: []string{"example.com", "registry.gitlab.com/group"}}

	if prefix, ok := p.matchExcludedRegistry("example.com/repo:tag"); !ok || prefix != "example.com" {
		t.Fatalf("expected example.com match, got prefix=%q ok=%v", prefix, ok)
	}
	if prefix, ok := p.matchExcludedRegistry("registry.gitlab.com/group/sub/app:latest"); !ok || prefix != "registry.gitlab.com/group" {
		t.Fatalf("expected registry.gitlab.com/group match, got prefix=%q ok=%v", prefix, ok)
	}
	if _, ok := p.matchExcludedRegistry("registry.gitlab.com/group-other/app:latest"); ok {
		t.Fatalf("expected group-other reference not to match")
	}
	if _, ok := p.matchExcludedRegistry("docker.io/library/nginx:latest"); ok {
		t.Fatalf("expected unrelated image not to match")
	}
}

func TestMirrorSkipsExcludedRegistry(t *testing.T) {
	p := &pusher{
		target:             fakeTarget{},
		excludedRegistries: []string{"example.com"},
		logger:             testr.New(t),
		keychain:           NewStaticKeychain(nil),
		pushed:             make(map[string]struct{}),
		failed:             make(map[string]time.Time),
	}

	if err := p.Mirror(context.Background(), "example.com/repo:tag", Metadata{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.pushed) != 0 {
		t.Fatalf("expected no targets to be recorded when skipping, got %d", len(p.pushed))
	}
}

func TestMirrorSkipsWithoutPodDigestWhenDigestPullEnabled(t *testing.T) {
	p := &pusher{
		target:         fakeTarget{prefix: "$namespace"},
		transform:      util.CleanRepoName,
		pullByDigest:   true,
		logger:         testr.New(t),
		keychain:       NewStaticKeychain(nil),
		pushed:         make(map[string]struct{}),
		failed:         make(map[string]time.Time),
		requestTimeout: 0,
	}

	if err := p.Mirror(context.Background(), "docker.io/library/nginx:1.28", Metadata{Namespace: "default"}); err != nil {
		t.Fatalf("expected skip without error, got %v", err)
	}
	if len(p.pushed) != 0 {
		t.Fatalf("expected target not to be marked as processed when skipping")
	}
}

func TestMirrorSkipsLoggingPushWhenDigestAlreadyPresent(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	normalized := fmt.Sprintf("docker.io/library/alpine@%s", digest)

	var headRefs []string
	var headMu sync.Mutex
	originalHead := remoteHeadFunc
	remoteHeadFunc = func(ref name.Reference, options ...remote.Option) (*v1.Descriptor, error) {
		headMu.Lock()
		headRefs = append(headRefs, ref.String())
		headMu.Unlock()
		return &v1.Descriptor{Digest: v1.Hash{Algorithm: "sha256", Hex: strings.TrimPrefix(digest, "sha256:")}}, nil
	}
	t.Cleanup(func() { remoteHeadFunc = originalHead })

	var logMu sync.Mutex
	var logMessages []string
	logger := funcr.New(func(prefix, args string) {
		logMu.Lock()
		defer logMu.Unlock()
		logMessages = append(logMessages, prefix+args)
	}, funcr.Options{Verbosity: 10})

	p := &pusher{
		target:       fakeTarget{prefix: "$namespace"},
		transform:    util.CleanRepoName,
		pullByDigest: true,
		logger:       logger,
		keychain:     NewStaticKeychain(nil),
		pushed:       make(map[string]struct{}),
		failed:       make(map[string]time.Time),
	}

	meta := Metadata{Namespace: "default", ImageID: normalized}
	if err := p.Mirror(context.Background(), "docker.io/library/alpine:3.19", meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headMu.Lock()
	defer headMu.Unlock()
	if len(headRefs) != 1 {
		t.Fatalf("expected exactly one HEAD request, got %d", len(headRefs))
	}
	if !strings.HasSuffix(headRefs[0], "@"+digest) {
		t.Fatalf("expected digest HEAD to target %s, got %s", digest, headRefs[0])
	}

	logMu.Lock()
	defer logMu.Unlock()
	for _, msg := range logMessages {
		if strings.Contains(msg, "pushing image to target") {
			t.Fatalf("unexpected push log message recorded: %s", msg)
		}
	}
}

func TestExpandRepoPrefixSkipsEmptySegments(t *testing.T) {
	cases := []struct {
		name string
		pref string
		meta Metadata
		want string
	}{
		{
			name: "all placeholders",
			pref: "$namespace/$podname",
			meta: Metadata{Namespace: "default"},
			want: "default",
		},
		{
			name: "static path",
			pref: "mirror",
			meta: Metadata{Namespace: "irrelevant"},
			want: "mirror",
		},
		{
			name: "trim spaces",
			pref: "  $namespace / $container_name  ",
			meta: Metadata{Namespace: "Team", ContainerName: "App"},
			want: "Team/App",
		},
		{
			name: "arch placeholder omitted when empty",
			pref: "$arch/$namespace",
			meta: Metadata{Namespace: "default"},
			want: "default",
		},
		{
			name: "arch placeholder applied",
			pref: "$arch/$namespace",
			meta: Metadata{Namespace: "default", Architecture: "amd64"},
			want: "amd64/default",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandRepoPrefix(tc.pref, tc.meta)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestBeginProcessingSkipsDuringCooldown(t *testing.T) {
	now := time.Now()
	target := "example.com/repo:tag"
	p := &pusher{
		dryRun:          false,
		pushed:          make(map[string]struct{}),
		failed:          map[string]time.Time{target: now.Add(-30 * time.Minute)},
		failureCooldown: time.Hour,
		now: func() time.Time {
			return now
		},
	}

	skip, err := p.beginProcessing(target, testr.New(t))
	if skip {
		t.Fatalf("expected skip to be false when in cooldown")
	}
	if err == nil {
		t.Fatalf("expected error while in cooldown")
	}
	retryErr, ok := err.(*RetryError)
	if !ok {
		t.Fatalf("expected RetryError, got %T", err)
	}
	expectedRetry := p.failed[target].Add(p.failureCooldown)
	if !retryErr.RetryAt.Equal(expectedRetry) {
		t.Fatalf("expected retry at %v, got %v", expectedRetry, retryErr.RetryAt)
	}
	if _, exists := p.pushed[target]; exists {
		t.Fatalf("expected target not to be marked as pushed")
	}
}

func TestBeginProcessingAllowsAfterCooldown(t *testing.T) {
	now := time.Now()
	target := "example.com/repo:tag"
	p := &pusher{
		dryRun:          false,
		pushed:          make(map[string]struct{}),
		failed:          map[string]time.Time{target: now.Add(-2 * time.Hour)},
		failureCooldown: time.Hour,
		now: func() time.Time {
			return now
		},
	}

	skip, err := p.beginProcessing(target, testr.New(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip {
		t.Fatalf("expected processing to continue after cooldown")
	}
	if _, exists := p.pushed[target]; !exists {
		t.Fatalf("expected target to be marked as pushed")
	}
	if _, failed := p.failed[target]; failed {
		t.Fatalf("expected failure record to be cleared after cooldown")
	}
}

func TestFailureResultRecordsState(t *testing.T) {
	now := time.Now()
	target := "example.com/repo:tag"
	p := &pusher{
		pushed:          map[string]struct{}{target: {}},
		failed:          make(map[string]time.Time),
		failureCooldown: time.Hour,
		now: func() time.Time {
			return now
		},
	}

	err := p.failureResult(target, assertError("boom"))
	retryErr, ok := err.(*RetryError)
	if !ok {
		t.Fatalf("expected RetryError, got %T", err)
	}
	expectedRetry := now.Add(time.Hour)
	if !retryErr.RetryAt.Equal(expectedRetry) {
		t.Fatalf("unexpected retry at %v", retryErr.RetryAt)
	}
	if _, exists := p.pushed[target]; exists {
		t.Fatalf("expected target to be removed from pushed set")
	}
	if ts, ok := p.failed[target]; !ok || ts != now {
		t.Fatalf("expected failure timestamp to be recorded, got %v", ts)
	}
}

func TestFailureResultWithoutCooldown(t *testing.T) {
	now := time.Now()
	target := "example.com/repo:tag"
	p := &pusher{
		pushed:          map[string]struct{}{target: {}},
		failed:          make(map[string]time.Time),
		failureCooldown: 0,
		now: func() time.Time {
			return now
		},
	}

	err := p.failureResult(target, assertError("boom"))
	if err == nil {
		t.Fatalf("expected failure result to return error")
	}
	if _, ok := err.(*RetryError); ok {
		t.Fatalf("expected plain error when cooldown disabled, got RetryError")
	}
	if _, exists := p.pushed[target]; exists {
		t.Fatalf("expected target to be removed from pushed set")
	}
	if _, recorded := p.failed[target]; recorded {
		t.Fatalf("expected failure state not to be recorded when cooldown disabled")
	}
}

func TestResetCooldown(t *testing.T) {
	target := "example.com/repo:tag"
	now := time.Now()
	p := &pusher{
		failureCooldown: time.Hour,
		failed: map[string]time.Time{
			target:                     now,
			"example.com/other:latest": now.Add(-time.Minute),
		},
	}

	cleared, enabled := p.ResetCooldown()
	if !enabled {
		t.Fatalf("expected cooldown to be reported as enabled")
	}
	if cleared != 2 {
		t.Fatalf("expected 2 cleared entries, got %d", cleared)
	}
	if len(p.failed) != 0 {
		t.Fatalf("expected failure map to be empty, got %d entries", len(p.failed))
	}

	cleared, enabled = p.ResetCooldown()
	if !enabled {
		t.Fatalf("expected cooldown to remain enabled")
	}
	if cleared != 0 {
		t.Fatalf("expected zero cleared entries on second reset, got %d", cleared)
	}

	p.failureCooldown = 0
	cleared, enabled = p.ResetCooldown()
	if enabled {
		t.Fatalf("expected cooldown to be reported as disabled")
	}
	if cleared != 0 {
		t.Fatalf("expected zero cleared entries when cooldown disabled, got %d", cleared)
	}
}

type assertError string

func (a assertError) Error() string { return string(a) }

func TestDetectRegistryAuthErrorByStatus(t *testing.T) {
	transportErr := &remotetransport.Error{StatusCode: http.StatusUnauthorized}

	info, ok := detectRegistryAuthError(transportErr)
	if !ok {
		t.Fatalf("expected registry auth error to be detected")
	}
	if info.statusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status code: %d", info.statusCode)
	}
	if len(info.diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", info.diagnostics)
	}
}

func TestDetectRegistryAuthErrorByDiagnostic(t *testing.T) {
	transportErr := &remotetransport.Error{
		StatusCode: http.StatusBadRequest,
		Errors: []remotetransport.Diagnostic{
			{Code: remotetransport.UnauthorizedErrorCode, Message: "authentication required"},
		},
	}

	info, ok := detectRegistryAuthError(transportErr)
	if !ok {
		t.Fatalf("expected registry auth error to be detected")
	}
	if info.statusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", info.statusCode)
	}
	if len(info.diagnostics) != 1 {
		t.Fatalf("expected a single diagnostic entry, got %d", len(info.diagnostics))
	}
	want := "UNAUTHORIZED: authentication required"
	if info.diagnostics[0] != want {
		t.Fatalf("unexpected diagnostic: %q", info.diagnostics[0])
	}
}
