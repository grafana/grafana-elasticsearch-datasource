package instrumentation

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func gaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, gauge.Write(&m))
	return m.GetGauge().GetValue()
}

func TestDatasourceInstances(t *testing.T) {
	t.Run("Should track active instances per distribution and major version", func(t *testing.T) {
		gauge := DatasourceInstances.WithLabelValues("elasticsearch", "9")

		before := gaugeValue(t, gauge)

		DatasourceInstances.WithLabelValues("elasticsearch", "9").Inc()
		assert.Equal(t, before+1, gaugeValue(t, gauge))

		DatasourceInstances.WithLabelValues("elasticsearch", "9").Dec()
		assert.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("Should track distributions independently", func(t *testing.T) {
		other := DatasourceInstances.WithLabelValues("customdistro", "2")
		before := gaugeValue(t, other)

		DatasourceInstances.WithLabelValues("elasticsearch", "8").Inc()

		assert.Equal(t, before, gaugeValue(t, other))
	})
}

func TestSanitizeDistribution(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "elasticsearch passes through", input: "elasticsearch", want: "elasticsearch"},
		{name: "elasticsearch serverless passes through", input: "elasticsearch_serverless", want: "elasticsearch_serverless"},
		{name: "tagline-detected distribution passes through", input: es.DistributionTagline, want: es.DistributionTagline},
		{name: "unknown passes through", input: "unknown", want: "unknown"},
		{name: "empty string maps to other", input: "", want: "other"},
		{name: "markup maps to other", input: "Elasticsearch<script>", want: "other"},
		{name: "long string maps to other", input: strings.Repeat("a", 500), want: "other"},
		{name: "arbitrary word maps to other", input: "banana", want: "other"},
		{name: "other itself maps to other", input: "other", want: "other"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeDistribution(tc.input))
		})
	}
}

func TestSanitizeVersionMajor(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single digit passes through", input: "8", want: "8"},
		{name: "major extracted from prerelease passes through", input: "9", want: "9"},
		{name: "leading zero is normalised", input: "08", want: "8"},
		{name: "upper bound passes through", input: "99", want: "99"},
		{name: "zero passes through", input: "0", want: "0"},
		{name: "full version string maps to unknown", input: "9.0.0-beta1", want: "unknown"},
		{name: "empty string maps to unknown", input: "", want: "unknown"},
		{name: "arbitrary word maps to unknown", input: "banana", want: "unknown"},
		{name: "negative maps to unknown", input: "-1", want: "unknown"},
		{name: "out of range maps to unknown", input: "999", want: "unknown"},
		{name: "long string maps to unknown", input: strings.Repeat("1", 500), want: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SanitizeVersionMajor(tc.input))
		})
	}
}
