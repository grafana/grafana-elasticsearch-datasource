package featureflags

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Outcome labels for evaluationsTotal. Singleflight followers that piggyback
// on an in-flight request increment none of them, so hit counts cache reads
// and miss and error count actual OFREP requests.
const (
	outcomeHit   = "hit"
	outcomeMiss  = "miss"
	outcomeError = "error"
)

var (
	evaluationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "grafana",
		Name:      "elasticsearch_plugin_feature_flag_evaluations_total",
		Help:      "Feature flag evaluations by flag and outcome (hit, miss, error)",
	}, []string{"flag", "outcome"})

	// requestDurationSeconds is observed on the cache-miss path only; a cache
	// hit performs no I/O. This is the latency a query pays once per flag and
	// tenant per cache TTL window.
	requestDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "grafana",
		Name:      "elasticsearch_plugin_feature_flag_request_duration_seconds",
		Help:      "Duration of OFREP feature flag requests in seconds",
		Buckets:   []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"flag"})
)
