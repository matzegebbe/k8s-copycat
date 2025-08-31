package util

import "testing"

func TestNewRepoPathTransformer(t *testing.T) {
	mappings := []PathMapping{
		{From: "old/", To: "new/"},
		{From: "^legacy/(.*)", To: "modern/$1", Regex: true},
	}
	transform := NewRepoPathTransformer(mappings)

	if got := transform("old/repo"); got != "new/repo" {
		t.Fatalf("prefix mapping failed: got %q", got)
	}
	if got := transform("legacy/service"); got != "modern/service" {
		t.Fatalf("regex mapping failed: got %q", got)
	}
}
