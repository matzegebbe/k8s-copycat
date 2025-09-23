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

	if got := testutil.ToFloat64(PullSuccessCounter().WithLabelValues(image)); got != 1 {
		t.Fatalf("expected pull counter to be 1, got %v", got)
	}
}

func TestRecordPushSuccessIncrementsCounter(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	image := "registry.internal/prod/app@sha256:deadbeef"
	RecordPushSuccess(image)

	if got := testutil.ToFloat64(PushSuccessCounter().WithLabelValues(image)); got != 1 {
		t.Fatalf("expected push counter to be 1, got %v", got)
	}
}

func TestRecordIgnoresEmptyImage(t *testing.T) {
	t.Cleanup(Reset)
	Reset()

	RecordPullSuccess("")
	RecordPushSuccess("")

	if count := testutil.CollectAndCount(PullSuccessCounter()); count != 0 {
		t.Fatalf("expected pull counter to remain empty, got %d samples", count)
	}
	if count := testutil.CollectAndCount(PushSuccessCounter()); count != 0 {
		t.Fatalf("expected push counter to remain empty, got %d samples", count)
	}
}
