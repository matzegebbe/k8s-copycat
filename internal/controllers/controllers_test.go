package controllers

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/matzegebbe/k8s-copycat/internal/mirror"
	"github.com/matzegebbe/k8s-copycat/pkg/util"
	ctrl "sigs.k8s.io/controller-runtime"
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
	daemonSet := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "node-agent", Namespace: "default"}}

	podFromDeployment := podWithOwner("default", "copycat-abc-def", appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), "copycat-abc")
	podFromJob := podWithOwner("default", "nightly-123-xyz", batchv1.SchemeGroupVersion.WithKind("Job"), "nightly-123")
	podFromStatefulSet := podWithOwner("default", "db-0", appsv1.SchemeGroupVersion.WithKind("StatefulSet"), "db")
	podFromDaemonSet := podWithOwner("default", "node-agent-123", appsv1.SchemeGroupVersion.WithKind("DaemonSet"), "node-agent")
	allowedPod := podWithOwner("default", "other-123", appsv1.SchemeGroupVersion.WithKind("ReplicaSet"), "other-123")

	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		deployment, replicaSet, cronJob, job, statefulSet, daemonSet,
	).Build()

	reconciler := PodReconciler{baseReconciler{
		Client:            client,
		AllowedNamespaces: []string{"*"},
		SkippedNamespaces: map[string]struct{}{},
		SkipDeployments:   newNameMatcher([]string{"copycat"}),
		SkipStatefulSets:  newNameMatcher([]string{"db"}),
		SkipDaemonSets:    newNameMatcher([]string{"node-agent"}),
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
	if skip, err := reconciler.shouldSkipPod(ctx, podFromDaemonSet); err != nil || !skip {
		t.Fatalf("expected daemonset pod to be skipped, skip=%v err=%v", skip, err)
	}
	if skip, err := reconciler.shouldSkipPod(ctx, allowedPod); err != nil || skip {
		t.Fatalf("expected unrelated pod not to be skipped, skip=%v err=%v", skip, err)
	}
}

func TestParseWatchResources(t *testing.T) {
	t.Parallel()

	parsed, invalid := ParseWatchResources([]string{"Pods", "deployments", "pods", "DaemonSets"})
	if len(invalid) != 0 {
		t.Fatalf("expected no invalid entries, got %v", invalid)
	}
	expected := []ResourceType{ResourcePods, ResourceDeployments, ResourceDaemonSets}
	if !reflect.DeepEqual(parsed, expected) {
		t.Fatalf("unexpected parse result: %v", parsed)
	}

	parsed, invalid = ParseWatchResources([]string{"unknown", ""})
	if len(parsed) != 0 {
		t.Fatalf("expected no parsed entries for invalid input, got %v", parsed)
	}
	if !reflect.DeepEqual(invalid, []string{"unknown"}) {
		t.Fatalf("unexpected invalid list: %v", invalid)
	}
}

func TestMirrorPodImagesContinuesAfterError(t *testing.T) {
	retry := &mirror.RetryError{Cause: errors.New("transient"), RetryAt: time.Now().Add(time.Minute)}
	otherErr := errors.New("permanent")
	pusher := &recordingPusher{responses: []error{retry, nil, otherErr}}

	images := []util.PodImage{
		{Image: "docker.io/library/a:v1", ContainerName: "a"},
		{Image: "docker.io/library/b:v1", ContainerName: "b"},
		{Image: "docker.io/library/c:v1", ContainerName: "c"},
	}

	r := baseReconciler{Pusher: pusher}
	ctx := ctrl.LoggerInto(context.Background(), testr.New(t))

	mirrored, err := r.mirrorPodImages(ctx, "default", "pod", images)
	if mirrored != 1 {
		t.Fatalf("expected exactly one successful mirror, got %d", mirrored)
	}
	if len(pusher.calls) != len(images) {
		t.Fatalf("expected pusher to be invoked for every image, got %d calls", len(pusher.calls))
	}
	var gotRetry *mirror.RetryError
	if !errors.As(err, &gotRetry) {
		t.Fatalf("expected retry error, got %v", err)
	}
	if gotRetry != retry {
		t.Fatalf("expected retry error pointer to be preserved")
	}
}

func TestMirrorPodImagesReturnsFirstErrorWithoutRetry(t *testing.T) {
	firstErr := errors.New("boom")
	pusher := &recordingPusher{responses: []error{firstErr, nil}}
	images := []util.PodImage{
		{Image: "docker.io/library/a:v1", ContainerName: "a"},
		{Image: "docker.io/library/b:v1", ContainerName: "b"},
	}

	r := baseReconciler{Pusher: pusher}
	ctx := ctrl.LoggerInto(context.Background(), testr.New(t))

	mirrored, err := r.mirrorPodImages(ctx, "default", "pod", images)
	if mirrored != 1 {
		t.Fatalf("expected one successful mirror, got %d", mirrored)
	}
	if len(pusher.calls) != len(images) {
		t.Fatalf("expected pusher to be called for both images, got %d", len(pusher.calls))
	}
	if err != firstErr {
		t.Fatalf("expected first error to be returned, got %v", err)
	}
}

type recordingPusher struct {
	responses []error
	calls     []string
}

func (p *recordingPusher) Mirror(_ context.Context, sourceImage string, _ mirror.Metadata) error {
	p.calls = append(p.calls, sourceImage)
	if len(p.responses) == 0 {
		return nil
	}
	err := p.responses[0]
	p.responses = p.responses[1:]
	return err
}

func (*recordingPusher) DryRun() bool { return false }

func (*recordingPusher) DryPull() bool { return false }

func (*recordingPusher) ResetCooldown() (int, bool) { return 0, false }

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
