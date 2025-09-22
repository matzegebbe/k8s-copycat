package controllers

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNameMatcherMatches(t *testing.T) {
	matcher := newNameMatcher([]string{"copycat", "prod/only", "dev/app"})
	if !matcher.matches("default", "copycat") {
		t.Fatalf("expected global match for copycat")
	}
	if !matcher.matches("prod", "only") {
		t.Fatalf("expected namespace specific match for prod/only")
	}
	if !matcher.matches("dev", "app") {
		t.Fatalf("expected namespace specific match for dev/app")
	}
	wildcard := newNameMatcher([]string{"*"})
	if !wildcard.matches("ns", "anything") {
		t.Fatalf("expected wildcard match when * present")
	}
}

func TestNamespaceFiltering(t *testing.T) {
	r := baseReconciler{
		AllowedNamespaces: []string{"*"},
		SkippedNamespaces: map[string]struct{}{"kube-system": {}},
	}
	if r.nsAllowed("kube-system") {
		t.Fatalf("expected kube-system to be skipped")
	}
	if !r.nsAllowed("default") {
		t.Fatalf("expected default to be allowed")
	}

	r = baseReconciler{
		AllowedNamespaces: []string{"prod", "default"},
		SkippedNamespaces: map[string]struct{}{"prod": {}},
	}
	if r.nsAllowed("prod") {
		t.Fatalf("expected prod to be skipped despite allow list")
	}
	if !r.nsAllowed("default") {
		t.Fatalf("expected default to be allowed from allow list")
	}
	if r.nsAllowed("dev") {
		t.Fatalf("expected dev to be disallowed")
	}
}

func TestPodReconcilerShouldSkip(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add apps scheme: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add batch scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "copycat", Namespace: "default"}}
	replicaSet := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{
		Name:      "copycat-abc",
		Namespace: "default",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
			Name:       "copycat",
		}},
	}}
	cronJob := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"}}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name:      "nightly-123",
		Namespace: "default",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: batchv1.SchemeGroupVersion.String(),
			Kind:       "CronJob",
			Name:       "nightly",
		}},
	}}
	statefulSet := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"}}

	podFromDeployment := podWithOwner("default", "copycat-abc-def", appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), "copycat-abc")
	podFromJob := podWithOwner("default", "nightly-123-xyz", batchv1.SchemeGroupVersion.WithKind("Job"), "nightly-123")
	podFromStatefulSet := podWithOwner("default", "db-0", appsv1.SchemeGroupVersion.WithKind("StatefulSet"), "db")
	allowedPod := podWithOwner("default", "other-123", appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), "other-123")

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		deployment, replicaSet, cronJob, job, statefulSet,
	).Build()

	reconciler := PodReconciler{baseReconciler{
		Client:            client,
		AllowedNamespaces: []string{"*"},
		SkippedNamespaces: map[string]struct{}{},
		SkipDeployments:   newNameMatcher([]string{"copycat"}),
		SkipStatefulSets:  newNameMatcher([]string{"db"}),
		SkipJobs:          newNameMatcher(nil),
		SkipCronJobs:      newNameMatcher([]string{"nightly"}),
		SkipPods:          newNameMatcher(nil),
	}}

	ctx := context.Background()

	if skip, err := reconciler.shouldSkipPod(ctx, podFromDeployment); err != nil || !skip {
		t.Fatalf("expected deployment pod to be skipped, skip=%v err=%v", skip, err)
	}
	if skip, err := reconciler.shouldSkipPod(ctx, podFromJob); err != nil || !skip {
		t.Fatalf("expected cronjob pod to be skipped, skip=%v err=%v", skip, err)
	}
	if skip, err := reconciler.shouldSkipPod(ctx, podFromStatefulSet); err != nil || !skip {
		t.Fatalf("expected statefulset pod to be skipped, skip=%v err=%v", skip, err)
	}
	if skip, err := reconciler.shouldSkipPod(ctx, allowedPod); err != nil || skip {
		t.Fatalf("expected unrelated pod not to be skipped, skip=%v err=%v", skip, err)
	}
}

func podWithOwner(namespace, name string, gvkt schema.GroupVersionKind, ownerName string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace: namespace,
		Name:      name,
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: gvkt.GroupVersion().String(),
			Kind:       gvkt.Kind,
			Name:       ownerName,
		}},
	}}
}
