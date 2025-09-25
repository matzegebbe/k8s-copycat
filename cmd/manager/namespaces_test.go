package main

import (
	"context"
	"testing"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestValidateAndExpandNamespaces(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-1"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-2"}},
	)

	ctx := context.Background()
	log := testr.New(t)

	expanded, err := validateAndExpandNamespaces(ctx, log, client, []string{"default", "test-*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"default", "test-1", "test-2"}
	if len(expanded) != len(want) {
		t.Fatalf("unexpected expanded length: want %d got %d", len(want), len(expanded))
	}
	for i, name := range want {
		if expanded[i] != name {
			t.Fatalf("unexpected namespace at index %d: want %s got %s", i, name, expanded[i])
		}
	}

	expanded, err = validateAndExpandNamespaces(ctx, log, client, []string{"missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(expanded) != 1 || expanded[0] != "missing" {
		t.Fatalf("expected missing namespace to be returned, got %#v", expanded)
	}

	expanded, err = validateAndExpandNamespaces(ctx, log, client, []string{"*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(expanded) != 1 || expanded[0] != "*" {
		t.Fatalf("expected wildcard to remain '*', got %#v", expanded)
	}
}

func TestMatchNamespacePatternInvalid(t *testing.T) {
	if _, err := matchNamespacePattern("test-[", []string{"test-1"}); err == nil {
		t.Fatalf("expected error for invalid pattern")
	}
}
