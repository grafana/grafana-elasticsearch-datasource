package elasticsearch

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// executeWithInterval runs a single query through the data query pipeline with the
// given panel interval and time range, returning the search request that was built.
func executeWithInterval(t *testing.T, body string, from, to time.Time, interval time.Duration) *es.SearchRequest {
	t.Helper()
	c := newFakeClient()
	dataRequest := backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				JSON:      json.RawMessage(body),
				TimeRange: backend.TimeRange{From: from, To: to},
				RefID:     "A",
				Interval:  interval,
			},
		},
	}
	query := newElasticsearchDataQuery(context.Background(), c, &dataRequest, log.New(), "")
	_, err := query.execute()
	require.NoError(t, err)
	require.Len(t, c.multisearchRequests, 1)
	require.Len(t, c.multisearchRequests[0].Requests, 1)
	return c.multisearchRequests[0].Requests[0]
}

func TestClampAutoIntervalToMaxBuckets(t *testing.T) {
	// A week-long range at a 10s interval produces ~60,480 time buckets. Nested under
	// a terms aggregation those multiply per terms bucket, blowing straight through
	// the Elasticsearch search.max_buckets default (65,535).
	weekFrom := time.Date(2018, 5, 8, 17, 50, 0, 0, time.UTC)
	weekTo := time.Date(2018, 5, 15, 17, 50, 0, 0, time.UTC)

	t.Run("terms agg with default size widens a too-fine auto interval", func(t *testing.T) {
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "0" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		// 500 terms buckets leave a budget of ~129 time buckets across 7 days,
		// which rounds up to a 2h fixed interval.
		require.Equal(t, sr.Interval, 2*time.Hour)
		// The date histogram still carries the placeholder; the widened interval is
		// substituted into it when the request is encoded.
		dateHistAgg := sr.Aggs[0].Aggregation.Aggs[0].Aggregation.Aggregation.(*es.DateHistogramAgg)
		require.Equal(t, dateHistAgg.FixedInterval, "$__interval_msms")
	})

	t.Run("interval that already fits is left alone", func(t *testing.T) {
		fiveMinFrom := time.Date(2018, 5, 15, 17, 50, 0, 0, time.UTC)
		fiveMinTo := time.Date(2018, 5, 15, 17, 55, 0, 0, time.UTC)
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "5" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, fiveMinFrom, fiveMinTo, 10*time.Second)
		require.Equal(t, sr.Interval, 10*time.Second)
	})

	t.Run("query without terms agg is left alone", func(t *testing.T) {
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "date_histogram", "field": "@timestamp", "id": "2", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		require.Equal(t, sr.Interval, 10*time.Second)
	})

	t.Run("explicit fixed interval is not clamped", func(t *testing.T) {
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "0" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "10s" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		require.Equal(t, sr.Interval, 10*time.Second)
	})

	t.Run("calendar interval is not clamped", func(t *testing.T) {
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "0" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "1M" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		require.Equal(t, sr.Interval, 10*time.Second)
	})

	t.Run("multiplier that alone exceeds the limit is left for Elasticsearch to reject", func(t *testing.T) {
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "0" } },
				{ "type": "terms", "field": "@instance", "id": "3", "settings": { "size": "0" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "4", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		require.Equal(t, sr.Interval, 10*time.Second)
	})

	t.Run("filters agg buckets count toward the clamp", func(t *testing.T) {
		monthFrom := time.Date(2018, 4, 15, 17, 50, 0, 0, time.UTC)
		monthTo := time.Date(2018, 5, 15, 17, 50, 0, 0, time.UTC)
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "filters", "id": "2", "settings": { "filters": [
					{ "query": "level:error" }, { "query": "level:warn" },
					{ "query": "level:info" }, { "query": "level:debug" }
				] } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, monthFrom, monthTo, 10*time.Second)
		// 4 filter buckets over 30 days budget ~16,381 time buckets, which rounds
		// up to a 5m fixed interval.
		require.Equal(t, sr.Interval, 5*time.Minute)
	})

	t.Run("zero interval is still clamped without dividing by zero", func(t *testing.T) {
		// Grafana sends no interval on some paths (Interval stays zero). The clamp
		// substitutes the encoder's one-second fallback for the bucket estimate;
		// without that substitution this test panics on integer division by zero.
		// The clamped result is independent of the incoming interval, so it matches
		// the 10s case above.
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "0" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 0)
		require.Equal(t, sr.Interval, 2*time.Hour)
	})

	t.Run("zero interval that fits the budget passes through unchanged", func(t *testing.T) {
		// Over five minutes the one-second fallback estimate fits the budget, so the
		// zero interval is returned as is for the request encoder to apply its own
		// one-second fallback at substitution time.
		fiveMinFrom := time.Date(2018, 5, 15, 17, 50, 0, 0, time.UTC)
		fiveMinTo := time.Date(2018, 5, 15, 17, 55, 0, 0, time.UTC)
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": "5" } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, fiveMinFrom, fiveMinTo, 0)
		require.Equal(t, sr.Interval, time.Duration(0))
	})

	t.Run("terms size given as a number is used for the clamp", func(t *testing.T) {
		// 100 differs from the 500 default on purpose: 100 terms buckets budget ~653
		// time buckets across 7 days, clamping to 30m instead of the 2h the default
		// would give, which proves the numeric size path fed the multiplier.
		sr := executeWithInterval(t, `{
			"bucketAggs": [
				{ "type": "terms", "field": "@host", "id": "2", "settings": { "size": 100 } },
				{ "type": "date_histogram", "field": "@timestamp", "id": "3", "settings": { "interval": "auto" } }
			],
			"metrics": [{ "type": "count", "id": "1" }]
		}`, weekFrom, weekTo, 10*time.Second)
		require.Equal(t, sr.Interval, 30*time.Minute)
	})
}
