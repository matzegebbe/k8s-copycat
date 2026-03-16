package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordPullSuccessIncrementsCounter(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	image := "registry.io/library/alpine:latest"
	RecordPullSuccess(image)

	if got := testutil.ToFloat64(PullSuccessCounter().WithLabelValues("registry.io")); got != 1 {
		t.Fatalf("expected pull counter to be 1, got %v", got)
	}
}

func TestRecordPullErrorIncrementsCounter(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	image := "registry.io/library/alpine:latest"
	RecordPullError(image)

	if got := testutil.ToFloat64(PullErrorCounter().WithLabelValues("registry.io")); got != 1 {
		t.Fatalf("expected pull error counter to be 1, got %v", got)
	}
}

func TestRecordPushSuccessIncrementsCounter(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	image := "registry.internal/prod/app@sha256:deadbeef"
	RecordPushSuccess(image)

	if got := testutil.ToFloat64(PushSuccessCounter().WithLabelValues("registry.internal")); got != 1 {
		t.Fatalf("expected push counter to be 1, got %v", got)
	}
}

func TestRecordPushErrorIncrementsCounter(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	image := "registry.internal/prod/app@sha256:deadbeef"
	RecordPushError(image)

	if got := testutil.ToFloat64(PushErrorCounter().WithLabelValues("registry.internal")); got != 1 {
		t.Fatalf("expected push error counter to be 1, got %v", got)
	}
}

func TestRecordIgnoresEmptyImage(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	RecordPullSuccess("")
	RecordPullError("")
	RecordPushSuccess("")
	RecordPushError("")

	if count := testutil.CollectAndCount(PullSuccessCounter()); count != 0 {
		t.Fatalf("expected pull counter to remain empty, got %d samples", count)
	}
	if count := testutil.CollectAndCount(PullErrorCounter()); count != 0 {
		t.Fatalf("expected pull error counter to remain empty, got %d samples", count)
	}
	if count := testutil.CollectAndCount(PushSuccessCounter()); count != 0 {
		t.Fatalf("expected push counter to remain empty, got %d samples", count)
	}
	if count := testutil.CollectAndCount(PushErrorCounter()); count != 0 {
		t.Fatalf("expected push error counter to remain empty, got %d samples", count)
	}
}

func TestRegistryLabelDefaultsDockerHub(t *testing.T) {
	if got := registryLabel("nginx:latest"); got != "docker.io" {
		t.Fatalf("expected docker.io registry label, got %q", got)
	}
}

func TestRegistryLabelHandlesSchemesAndPorts(t *testing.T) {
	if got := registryLabel("https://registry.internal:5000/team/app:1.2.3"); got != "registry.internal:5000" {
		t.Fatalf("expected registry.internal:5000 registry label, got %q", got)
	}
}
