package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	pullSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "app",
			Subsystem: "registry",
			Name:      "pull_success_total",
			Help:      "Total number of successful image pulls performed by k8s-copycat.",
		},
		[]string{"image"},
	)

	pushSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "app",
			Subsystem: "registry",
			Name:      "push_success_total",
			Help:      "Total number of successful image pushes performed by k8s-copycat.",
		},
		[]string{"image"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(pullSuccess, pushSuccess)
}

// RecordPullSuccess increments the pull success counter for the provided image.
func RecordPullSuccess(image string) {
	if image == "" {
		return
	}
	pullSuccess.WithLabelValues(image).Inc()
}

// RecordPushSuccess increments the push success counter for the provided image.
func RecordPushSuccess(image string) {
	if image == "" {
		return
	}
	pushSuccess.WithLabelValues(image).Inc()
}

// Reset clears internal metrics state. It is intended for use in tests only.
func Reset() {
	pullSuccess.Reset()
	pushSuccess.Reset()
}

// PullSuccessCounter returns the underlying prometheus counter for pull successes.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PullSuccessCounter() *prometheus.CounterVec {
	return pullSuccess
}

// PushSuccessCounter returns the underlying prometheus counter for push successes.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PushSuccessCounter() *prometheus.CounterVec {
	return pushSuccess
}
