package mirror

import (
	"context"
	"testing"

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
