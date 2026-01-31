package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	pullSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_copycat",
			Subsystem: "registry",
			Name:      "pull_success_total",
			Help:      "Total number of successful image pulls performed by k8s-copycat.",
		},
		[]string{"image"},
	)

	pullError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_copycat",
			Subsystem: "registry",
			Name:      "pull_error_total",
			Help:      "Total number of failed image pulls performed by k8s-copycat.",
		},
		[]string{"image"},
	)

	pushSuccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_copycat",
			Subsystem: "registry",
			Name:      "push_success_total",
			Help:      "Total number of successful image pushes performed by k8s-copycat.",
		},
		[]string{"image"},
	)

	pushError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "k8s_copycat",
			Subsystem: "registry",
			Name:      "push_error_total",
			Help:      "Total number of failed image pushes performed by k8s-copycat.",
		},
		[]string{"image"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(pullSuccess, pullError, pushSuccess, pushError)
}

// recordMetric increments the given counter for the provided image.
func recordMetric(counter *prometheus.CounterVec, image string) {
	if image == "" {
		return
	}
	counter.WithLabelValues(image).Inc()
}

// RecordPullSuccess increments the pull success counter for the provided image.
func RecordPullSuccess(image string) {
	recordMetric(pullSuccess, image)
}

// RecordPullError increments the pull error counter for the provided image.
func RecordPullError(image string) {
	recordMetric(pullError, image)
}

// RecordPushSuccess increments the push success counter for the provided image.
func RecordPushSuccess(image string) {
	recordMetric(pushSuccess, image)
}

// RecordPushError increments the push error counter for the provided image.
func RecordPushError(image string) {
	recordMetric(pushError, image)
}

// Reset clears internal metrics state. It is intended for use in tests only.
func Reset() {
	pullSuccess.Reset()
	pullError.Reset()
	pushSuccess.Reset()
	pushError.Reset()
}

// PullSuccessCounter returns the underlying prometheus counter for pull successes.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PullSuccessCounter() *prometheus.CounterVec {
	return pullSuccess
}

// PullErrorCounter returns the underlying prometheus counter for pull errors.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PullErrorCounter() *prometheus.CounterVec {
	return pullError
}

// PushSuccessCounter returns the underlying prometheus counter for push successes.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PushSuccessCounter() *prometheus.CounterVec {
	return pushSuccess
}

// PushErrorCounter returns the underlying prometheus counter for push errors.
// It is exposed for tests and advanced integrations that need direct access to the metric.
func PushErrorCounter() *prometheus.CounterVec {
	return pushError
}
