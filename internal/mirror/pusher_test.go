package mirror

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
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
