package instrumentation

import (
	"context"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

var (
	pluginParsingResponseDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "grafana",
		Name:      "elasticsearch_plugin_parse_response_duration_seconds",
		Help:      "Duration of Elasticsearch parsing the response in seconds",
		Buckets:   []float64{.001, 0.0025, .005, .0075, .01, .02, .03, .04, .05, .075, .1, .25, .5, 1, 5, 10, 25},
	}, []string{"status", "endpoint"})

	// DatasourceInstances tracks the currently active data source instances by
	// the distribution and major version detected from the cluster root
	// endpoint. Incremented when an instance is created and decremented when
	// the SDK disposes it.
	DatasourceInstances = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "grafana",
		Name:      "elasticsearch_plugin_datasource_instances",
		Help:      "Active Elasticsearch data source instances by detected cluster distribution and major version",
	}, []string{"distribution", "version_major"})
)

// distributionOther is the distribution label recorded for any value outside
// the known distribution set.
const distributionOther = "other"

// versionMajorUnknown is the version_major label recorded when the reported
// version does not parse as a sane major version. It deliberately matches
// es.DistributionUnknown so both label columns use the same fallback value.
const versionMajorUnknown = "unknown"

// SanitizeDistribution bounds the distribution label of DatasourceInstances to
// the known distribution set. The raw value is taken from the remote root
// endpoint response, so anything outside the set maps to "other" to stop a
// hostile or misconfigured endpoint minting unbounded metric series.
func SanitizeDistribution(distribution string) string {
	switch distribution {
	case es.DistributionElasticsearch, es.DistributionElasticsearchServerless, es.DistributionTagline, es.DistributionUnknown:
		return distribution
	default:
		return distributionOther
	}
}

// SanitizeVersionMajor bounds the version_major label of DatasourceInstances
// to integers between 0 and 99, normalised via strconv so "08" becomes "8".
// The raw value is derived from the remote root endpoint response, so anything
// else maps to "unknown" to stop unbounded metric series.
func SanitizeVersionMajor(versionMajor string) string {
	major, err := strconv.Atoi(versionMajor)
	if err != nil || major < 0 || major > 99 {
		return versionMajorUnknown
	}
	return strconv.Itoa(major)
}

func UpdatePluginParsingResponseDurationSeconds(ctx context.Context, duration time.Duration, status string) {
	histogram := pluginParsingResponseDurationSeconds.WithLabelValues(status, string(backend.EndpointQueryData))

	if traceID := tracing.TraceIDFromContext(ctx, true); traceID != "" {
		histogram.(prometheus.ExemplarObserver).ObserveWithExemplar(duration.Seconds(), prometheus.Labels{"traceID": traceID})
	} else {
		histogram.Observe(duration.Seconds())
	}
}
