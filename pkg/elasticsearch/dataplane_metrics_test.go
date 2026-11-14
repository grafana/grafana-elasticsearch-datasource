package elasticsearch

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/grafana/dataplane/sdata/numeric"
	"github.com/grafana/dataplane/sdata/timeseries"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

func stubMetricsDataplane(t *testing.T, enabled bool) {
	t.Helper()
	original := isMetricsDataplaneEnabled
	isMetricsDataplaneEnabled = func(context.Context) bool { return enabled }
	t.Cleanup(func() { isMetricsDataplaneEnabled = original })
}

// metricsFrames parses one query's response with the metrics dataplane flag
// set as given and returns its frames.
func metricsFrames(t *testing.T, target string, response string, dataplane bool) data.Frames {
	t.Helper()
	result, err := parseTestResponseWithFlags(map[string]string{"A": target}, response, false, false, dataplane)
	require.NoError(t, err)
	res, ok := result.Responses["A"]
	require.True(t, ok)
	require.NoError(t, res.Error)
	return res.Frames
}

// requireTimeSeriesMulti validates frames with the dataplane reader and data
// validation on: type indicator, []time.Time field, a numeric value field,
// unique name+labels per series and sorted time. It adds what the reader
// leaves out: the contract version and strictly increasing timestamps.
func requireTimeSeriesMulti(t *testing.T, frames data.Frames, wantSeries int) timeseries.Collection {
	t.Helper()
	reader, err := timeseries.CollectionReaderFromFrames(frames)
	require.NoError(t, err)
	collection, err := reader.GetCollection(true)
	require.NoError(t, err)
	require.NoError(t, collection.Warning)
	require.Len(t, collection.Refs, wantSeries)
	for i, frame := range frames {
		require.Equal(t, data.FrameTypeTimeSeriesMulti, frame.Meta.Type, "frame %d", i)
		require.Equal(t, dataplaneMetricsVersion, frame.Meta.TypeVersion, "frame %d", i)
		_, err := frame.RowLen()
		require.NoError(t, err, "frame %d", i)
		requireStrictlyIncreasingTime(t, frame)
	}
	return collection
}

func requireStrictlyIncreasingTime(t *testing.T, frame *data.Frame) {
	t.Helper()
	for _, idx := range frame.TypeIndices(data.FieldTypeTime) {
		field := frame.Fields[idx]
		for i := 1; i < field.Len(); i++ {
			prev, cur := field.At(i-1).(time.Time), field.At(i).(time.Time)
			require.Truef(t, cur.After(prev), "time %v at row %d is not after %v", cur, i, prev)
		}
	}
}

// requireNumericLong validates the single frame with the dataplane reader
// (data validation is unimplemented for this kind) and adds the contract
// checks the reader skips: version, no time fields, unique item identity.
func requireNumericLong(t *testing.T, frames data.Frames, wantItems int) numeric.Collection {
	t.Helper()
	require.Len(t, frames, 1)
	frame := frames[0]
	require.Equal(t, data.FrameTypeNumericLong, frame.Meta.Type)
	require.Equal(t, dataplaneMetricsVersion, frame.Meta.TypeVersion)
	require.Empty(t, frame.TypeIndices(data.FieldTypeTime, data.FieldTypeNullableTime))
	_, err := frame.RowLen()
	require.NoError(t, err)

	reader, err := numeric.CollectionReaderFromFrames(frames)
	require.NoError(t, err)
	collection, err := reader.GetCollection(false)
	require.NoError(t, err)
	require.NoError(t, collection.Warning)
	require.Len(t, collection.Refs, wantItems)
	seen := map[string]struct{}{}
	for _, ref := range collection.Refs {
		key := ref.GetMetricName() + "\x00" + ref.GetLabels().String()
		_, dup := seen[key]
		require.Falsef(t, dup, "duplicate item %q %v", ref.GetMetricName(), ref.GetLabels())
		seen[key] = struct{}{}
	}
	return collection
}

// requireTimeSeriesLong validates the single frame with data validation on,
// which rejects unsorted time and duplicate points per series.
func requireTimeSeriesLong(t *testing.T, frames data.Frames, wantSeries int) timeseries.Collection {
	t.Helper()
	require.Len(t, frames, 1)
	require.Equal(t, data.FrameTypeTimeSeriesLong, frames[0].Meta.Type)
	require.Equal(t, dataplaneMetricsVersion, frames[0].Meta.TypeVersion)
	reader, err := timeseries.CollectionReaderFromFrames(frames)
	require.NoError(t, err)
	collection, err := reader.GetCollection(true)
	require.NoError(t, err)
	require.NoError(t, collection.Warning)
	require.Len(t, collection.Refs, wantSeries)
	return collection
}

const termsDateHistogramTarget = `{
	"metrics": [{ "type": "count", "id": "1" }, { "type": "avg", "id": "3", "field": "bytes" }],
	"bucketAggs": [
		{ "type": "terms", "field": "host", "id": "2" },
		{ "type": "date_histogram", "field": "@timestamp", "id": "4" }
	]
}`

const termsDateHistogramResponse = `{"responses": [{"aggregations": {"2": {"buckets": [
	{"key": "server1", "doc_count": 4, "4": {"buckets": [
		{"key": 1000, "doc_count": 1, "3": {"value": 10}},
		{"key": 2000, "doc_count": 3, "3": {"value": 30}}]}},
	{"key": "server2", "doc_count": 10, "4": {"buckets": [
		{"key": 1000, "doc_count": 2, "3": {"value": 20}},
		{"key": 2000, "doc_count": 8, "3": {"value": 80}}]}}
]}}}]}`

const termsLeafTarget = `{
	"metrics": [{ "type": "count", "id": "1" }, { "type": "avg", "id": "3", "field": "bytes" }],
	"bucketAggs": [{ "type": "terms", "field": "status", "id": "2" }]
}`

const termsLeafResponse = `{"responses": [{"aggregations": {"2": {"buckets": [
	{"key": 200, "doc_count": 5, "3": {"value": 100}},
	{"key": 404, "doc_count": 2, "3": {"value": 50}}
]}}}]}`

const dateHistogramTermsTarget = `{
	"metrics": [{ "type": "count", "id": "1" }],
	"bucketAggs": [
		{ "type": "date_histogram", "field": "@timestamp", "id": "2", "settings": { "trimEdges": "0" } },
		{ "type": "terms", "field": "host", "id": "3" }
	]
}`

const dateHistogramTermsResponse = `{"responses": [{"aggregations": {"2": {"buckets": [
	{"key": 1000, "key_as_string": "1000", "doc_count": 3, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 2}]}},
	{"key": 2000, "key_as_string": "2000", "doc_count": 5, "3": {"buckets": [{"key": "a", "doc_count": 2}, {"key": "b", "doc_count": 3}]}}
]}}}]}`

func TestMetricsDataplaneFlagGatesQueryConstruction(t *testing.T) {
	c := newFakeClient()
	dataRequest := backend.QueryDataRequest{
		Queries: []backend.DataQuery{{JSON: json.RawMessage(`{}`), RefID: "A"}},
	}
	for _, enabled := range []bool{true, false} {
		stubMetricsDataplane(t, enabled)
		query := newElasticsearchDataQuery(context.Background(), c, &dataRequest, log.New(), "")
		require.Equal(t, enabled, query.metricsDataplaneEnabled)
	}
}

func TestMetricsDataplane_SeriesKeepLabelsAndItemNames(t *testing.T) {
	frames := metricsFrames(t, termsDateHistogramTarget, termsDateHistogramResponse, true)
	requireTimeSeriesMulti(t, frames, 4)

	want := []struct{ name, display, host string }{
		{"Count", "server1 Count", "server1"},
		{"Average bytes", "server1 Average bytes", "server1"},
		{"Count", "server2 Count", "server2"},
		{"Average bytes", "server2 Average bytes", "server2"},
	}
	require.Len(t, frames, len(want))
	for i, w := range want {
		value := frames[i].Fields[1]
		require.Equal(t, w.name, value.Name)
		require.Equal(t, data.Labels{"host": w.host}, value.Labels)
		require.Equal(t, w.display, value.Config.DisplayNameFromDS)
		require.Equal(t, w.display, frames[i].Name)
		require.Equal(t, data.TimeSeriesTimeFieldName, frames[i].Fields[0].Name)
	}
}

func TestMetricsDataplane_LegacyShapeUnchangedWhenOff(t *testing.T) {
	frames := metricsFrames(t, termsDateHistogramTarget, termsDateHistogramResponse, false)
	require.Len(t, frames, 4)
	for _, frame := range frames {
		require.Equal(t, data.FrameTypeTimeSeriesMulti, frame.Meta.Type)
		require.True(t, frame.Meta.TypeVersion.IsZero())
		require.Equal(t, data.TimeSeriesValueFieldName, frame.Fields[1].Name)
		require.Nil(t, frame.Fields[1].Labels)
	}
	require.Equal(t, "server1 Count", frames[0].Name)

	keepLabels, err := parseTestResponseWithFlags(map[string]string{"A": termsDateHistogramTarget}, termsDateHistogramResponse, true, false, false)
	require.NoError(t, err)
	wantDisplay := []string{"server1 Count", "server1 Average bytes", "server2 Count", "server2 Average bytes"}
	for i, frame := range keepLabels.Responses["A"].Frames {
		require.Equal(t, "", frame.Name)
		require.Equal(t, data.TimeSeriesValueFieldName, frame.Fields[1].Name)
		require.Equal(t, wantDisplay[i], frame.Fields[1].Config.DisplayNameFromDS)
	}
}

func TestMetricsDataplane_KeepLabelsInResponseDoesNotChangeShape(t *testing.T) {
	plain, err := parseTestResponseWithFlags(map[string]string{"A": termsDateHistogramTarget}, termsDateHistogramResponse, false, false, true)
	require.NoError(t, err)
	keep, err := parseTestResponseWithFlags(map[string]string{"A": termsDateHistogramTarget}, termsDateHistogramResponse, true, false, true)
	require.NoError(t, err)
	require.Equal(t, plain.Responses["A"].Frames, keep.Responses["A"].Frames)
	requireTimeSeriesMulti(t, keep.Responses["A"].Frames, 4)
}

func TestMetricsDataplane_DuplicateItemNamesGetMetricIDSuffix(t *testing.T) {
	target := `{
		"metrics": [
			{ "type": "avg", "id": "1", "field": "bytes" },
			{ "type": "avg", "id": "2", "field": "bytes" },
			{ "type": "extended_stats", "id": "3", "field": "bytes", "meta": { "avg": true } }
		],
		"bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "4" }]
	}`
	response := `{"responses": [{"aggregations": {"4": {"buckets": [
		{"key": 1000, "doc_count": 1, "1": {"value": 1}, "2": {"value": 2},
		 "3": {"count": 1, "min": 3, "max": 3, "avg": 3, "sum": 3, "std_deviation": 0, "std_deviation_bounds": {"upper": 3, "lower": 3}}}
	]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesMulti(t, frames, 3)
	require.Equal(t, "Average bytes", frames[0].Fields[1].Name)
	require.Equal(t, "Average bytes 2", frames[1].Fields[1].Name)
	require.Equal(t, "Average bytes 3", frames[2].Fields[1].Name)
	for _, frame := range frames {
		require.Nil(t, frame.Fields[1].Labels)
	}
}

func TestMetricsDataplane_AliasAffectsDisplayNameOnly(t *testing.T) {
	target := `{
		"alias": "{{host}} - {{metric}}",
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "terms", "field": "host", "id": "2" },
			{ "type": "date_histogram", "field": "@timestamp", "id": "4" }
		]
	}`
	frames := metricsFrames(t, target, termsDateHistogramResponse, true)
	requireTimeSeriesMulti(t, frames, 2)
	value := frames[0].Fields[1]
	require.Equal(t, "Count", value.Name)
	require.Equal(t, "server1 - Count", value.Config.DisplayNameFromDS)
	require.Equal(t, data.Labels{"host": "server1"}, value.Labels)
}

func TestMetricsDataplane_PercentilesAndPipelineSeries(t *testing.T) {
	target := `{
		"metrics": [
			{ "type": "max", "id": "1", "field": "latency" },
			{ "type": "percentiles", "id": "2", "field": "latency", "settings": { "percents": ["50", "95"] } },
			{ "type": "bucket_script", "id": "3", "pipelineVariables": [{ "name": "var1", "pipelineAgg": "1" }], "settings": { "script": "params.var1 * 2" } }
		],
		"bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "4" }]
	}`
	response := `{"responses": [{"aggregations": {"4": {"buckets": [
		{"key": 1000, "doc_count": 2, "1": {"value": 5}, "2": {"values": {"50.0": 1.5, "95.0": 9.5}}, "3": {"value": 10}},
		{"key": 2000, "doc_count": 3, "1": {"value": 7}, "2": {"values": {"50.0": 2.5, "95.0": 19.5}}, "3": {"value": 14}}
	]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesMulti(t, frames, 4)
	names := make([]string, 0, len(frames))
	for _, frame := range frames {
		names = append(names, frame.Fields[1].Name)
	}
	require.Equal(t, []string{"Max latency", "p50.0 latency", "p95.0 latency", "Max latency * 2"}, names)
}

func TestMetricsResponse_TopMetricsEmptyBucketKeepsVectorsAligned(t *testing.T) {
	target := `{
		"metrics": [{ "type": "top_metrics", "id": "1", "settings": { "metrics": ["cpu"], "order": "desc", "orderBy": "@timestamp" } }],
		"bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "2" }]
	}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 1000, "doc_count": 1, "1": {"top": [{"sort": [1000], "metrics": {"cpu": 0.5}}]}},
		{"key": 2000, "doc_count": 0, "1": {"top": []}},
		{"key": 3000, "doc_count": 1, "1": {"top": [{"sort": [3000], "metrics": {"cpu": 0.7}}]}}
	]}}}]}`
	for _, dataplane := range []bool{false, true} {
		frames := metricsFrames(t, target, response, dataplane)
		require.Len(t, frames, 1)
		rows, err := frames[0].RowLen()
		require.NoError(t, err)
		require.Equal(t, 3, rows)
		require.Nil(t, frames[0].Fields[1].At(1).(*float64))
		requireFloatAt(t, 0.7, frames[0].Fields[1], 2)
	}
	requireTimeSeriesMulti(t, metricsFrames(t, target, response, true), 1)
}

func TestMetricsDataplane_TermsTableIsNumericLongWithStringKeys(t *testing.T) {
	frames := metricsFrames(t, termsLeafTarget, termsLeafResponse, true)
	requireNumericLong(t, frames, 4)
	status := fieldByName(t, frames[0], "status")
	require.Equal(t, data.FieldTypeNullableString, status.Type())
	requireStringAt(t, "200", status, 0)
	requireStringAt(t, "404", status, 1)
	requireFloatAt(t, 5, fieldByName(t, frames[0], "Count"), 0)
	// table columns keep the legacy metric-only name; series items carry the field too
	requireFloatAt(t, 50, fieldByName(t, frames[0], "Average"), 1)

	legacy := metricsFrames(t, termsLeafTarget, termsLeafResponse, false)
	require.Len(t, legacy, 1)
	require.Nil(t, legacy[0].Meta)
	require.Equal(t, data.FieldTypeNullableFloat64, fieldByName(t, legacy[0], "status").Type())
}

func TestMetricsDataplane_LeafBucketKeysBecomeStringDimensions(t *testing.T) {
	tests := []struct {
		name, target, response, keyField string
		keys                             []string
	}{
		{
			name:     "keyed filters",
			target:   `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "filters", "id": "2", "settings": { "filters": [{ "label": "a", "query": "x:1" }, { "label": "b", "query": "x:2" }] } }]}`,
			response: `{"responses": [{"aggregations": {"2": {"buckets": {"a": {"doc_count": 12}, "b": {"doc_count": 39}}}}}]}`,
			keyField: "filter",
			keys:     []string{"a", "b"},
		},
		{
			name:     "fractional and large histogram keys",
			target:   `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "histogram", "field": "bytes", "id": "2", "settings": { "interval": "0.5" } }]}`,
			response: `{"responses": [{"aggregations": {"2": {"buckets": [{"key": 0.5, "doc_count": 1}, {"key": 1, "doc_count": 2}, {"key": 1000000, "doc_count": 3}]}}}]}`,
			keyField: "bytes",
			keys:     []string{"0.5", "1", "1000000"},
		},
		{
			name:     "terms without a field",
			target:   `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "terms", "id": "2", "settings": { "script": "doc['a'].value" } }]}`,
			response: `{"responses": [{"aggregations": {"2": {"buckets": [{"key": "x", "doc_count": 1}, {"key": "y", "doc_count": 2}]}}}]}`,
			keyField: "key",
			keys:     []string{"x", "y"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := metricsFrames(t, tt.target, tt.response, true)
			requireNumericLong(t, frames, len(tt.keys))
			field := fieldByName(t, frames[0], tt.keyField)
			for i, key := range tt.keys {
				requireStringAt(t, key, field, i)
			}
		})
	}
}

func TestMetricsDataplane_DateHistogramThenTermsIsTimeSeriesLong(t *testing.T) {
	frames := metricsFrames(t, dateHistogramTermsTarget, dateHistogramTermsResponse, true)
	collection := requireTimeSeriesLong(t, frames, 2)
	frame := frames[0]
	timeField := fieldByName(t, frame, "@timestamp")
	require.Equal(t, data.FieldTypeTime, timeField.Type())
	require.Equal(t, time.UnixMilli(1000).UTC(), timeField.At(0).(time.Time))
	require.Equal(t, time.UnixMilli(2000).UTC(), timeField.At(3).(time.Time))
	requireStringAt(t, "a", fieldByName(t, frame, "host"), 0)
	requireFloatAt(t, 3, fieldByName(t, frame, "Count"), 3)
	for _, ref := range collection.Refs {
		require.Equal(t, "Count", ref.GetMetricName())
		n, err := ref.Len()
		require.NoError(t, err)
		require.Equal(t, 2, n)
	}

	legacy := metricsFrames(t, dateHistogramTermsTarget, dateHistogramTermsResponse, false)
	require.Len(t, legacy, 1)
	require.Nil(t, legacy[0].Meta)
	require.Equal(t, data.FieldTypeNullableString, fieldByName(t, legacy[0], "@timestamp").Type())
}

func TestMetricsDataplane_LongFrameRowsAreSortedByTime(t *testing.T) {
	target := `{
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "terms", "field": "host", "id": "2" },
			{ "type": "date_histogram", "field": "@timestamp", "id": "3" },
			{ "type": "terms", "field": "status", "id": "4" }
		]
	}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": "a", "doc_count": 4, "3": {"buckets": [
			{"key": 1000, "doc_count": 2, "4": {"buckets": [{"key": 200, "doc_count": 1}, {"key": 404, "doc_count": 1}]}},
			{"key": 2000, "doc_count": 2, "4": {"buckets": [{"key": 200, "doc_count": 1}, {"key": 404, "doc_count": 1}]}}]}},
		{"key": "b", "doc_count": 4, "3": {"buckets": [
			{"key": 1000, "doc_count": 2, "4": {"buckets": [{"key": 200, "doc_count": 1}, {"key": 404, "doc_count": 1}]}},
			{"key": 2000, "doc_count": 2, "4": {"buckets": [{"key": 200, "doc_count": 1}, {"key": 404, "doc_count": 1}]}}]}}
	]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesLong(t, frames, 4)
	timeField := fieldByName(t, frames[0], "@timestamp")
	host := fieldByName(t, frames[0], "host")
	require.Equal(t, 8, timeField.Len())
	for i := 1; i < timeField.Len(); i++ {
		require.False(t, timeField.At(i).(time.Time).Before(timeField.At(i-1).(time.Time)))
	}
	// rows sharing a timestamp keep their response order: host a before host b
	requireStringAt(t, "a", host, 0)
	requireStringAt(t, "b", host, 2)
	requireStringAt(t, "404", fieldByName(t, frames[0], "status"), 1)
}

func TestMetricsDataplane_LongFrameTrimEdgesDropsWholeTimestamps(t *testing.T) {
	target := `{
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "date_histogram", "field": "@timestamp", "id": "2", "settings": { "trimEdges": "1" } },
			{ "type": "terms", "field": "host", "id": "3" }
		]
	}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 1000, "doc_count": 2, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 1}]}},
		{"key": 2000, "doc_count": 2, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 1}]}},
		{"key": 3000, "doc_count": 2, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 1}]}}
	]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesLong(t, frames, 2)
	timeField := fieldByName(t, frames[0], "@timestamp")
	require.Equal(t, 2, timeField.Len())
	require.Equal(t, time.UnixMilli(2000).UTC(), timeField.At(0).(time.Time))
	require.Equal(t, time.UnixMilli(2000).UTC(), timeField.At(1).(time.Time))
}

func TestMetricsResponse_EmptyInnerBucketsKeepEarlierRows(t *testing.T) {
	target := `{
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "terms", "field": "host", "id": "2" },
			{ "type": "terms", "field": "status", "id": "3" }
		]
	}`
	full := func(host string) string {
		return `{"key": "` + host + `", "doc_count": 2, "3": {"buckets": [{"key": "200", "doc_count": 1}, {"key": "404", "doc_count": 1}]}}`
	}
	empty := func(host string) string {
		return `{"key": "` + host + `", "doc_count": 0, "3": {"buckets": []}}`
	}
	for name, buckets := range map[string]string{
		"empty last":   full("a") + "," + full("b") + "," + empty("c"),
		"empty middle": full("a") + "," + empty("b") + "," + full("c"),
	} {
		t.Run(name, func(t *testing.T) {
			response := `{"responses": [{"aggregations": {"2": {"buckets": [` + buckets + `]}}}]}`
			for _, dataplane := range []bool{false, true} {
				frames := metricsFrames(t, target, response, dataplane)
				require.Len(t, frames, 1)
				requireFrameLength(t, frames[0], 4)
			}
			requireNumericLong(t, metricsFrames(t, target, response, true), 4)
		})
	}
}

func TestMetricsResponse_DuplicateTableColumnNamesGetMetricIDSuffix(t *testing.T) {
	target := `{
		"metrics": [{ "type": "count", "id": "1" }, { "type": "count", "id": "5" }],
		"bucketAggs": [{ "type": "terms", "field": "host", "id": "2" }]
	}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 2}]}}}]}`
	for _, dataplane := range []bool{false, true} {
		frames := metricsFrames(t, target, response, dataplane)
		require.Len(t, frames, 1)
		requireFrameLength(t, frames[0], 2)
		requireFloatAt(t, 2, fieldByName(t, frames[0], "Count"), 1)
		requireFloatAt(t, 2, fieldByName(t, frames[0], "Count 5"), 1)
	}
	requireNumericLong(t, metricsFrames(t, target, response, true), 4)
}

func TestMetricsResponse_OuterNumericKeysKeepFractions(t *testing.T) {
	target := `{
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "histogram", "field": "ratio", "id": "2", "settings": { "interval": "0.25" } },
			{ "type": "date_histogram", "field": "@timestamp", "id": "3" }
		]
	}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 0.5, "doc_count": 1, "3": {"buckets": [{"key": 1000, "doc_count": 1}]}},
		{"key": 0.75, "doc_count": 1, "3": {"buckets": [{"key": 1000, "doc_count": 1}]}}
	]}}}]}`
	legacy := metricsFrames(t, target, response, false)
	require.Len(t, legacy, 2)
	require.Equal(t, "0.5", legacy[0].Name)
	require.Equal(t, "0.75", legacy[1].Name)

	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesMulti(t, frames, 2)
	require.Equal(t, data.Labels{"ratio": "0.5"}, frames[0].Fields[1].Labels)
	require.Equal(t, data.Labels{"ratio": "0.75"}, frames[1].Fields[1].Labels)
}

func TestMetricsDataplane_NoDataIsASingleTypedFrame(t *testing.T) {
	noBuckets := `{"responses": [{"aggregations": {"2": {"buckets": []}}}]}`
	tests := []struct {
		name, target string
		frameType    data.FrameType
	}{
		{"terms leaf", termsLeafTarget, data.FrameTypeNumericLong},
		{"date histogram then terms", dateHistogramTermsTarget, data.FrameTypeTimeSeriesLong},
		{"terms then date histogram", termsDateHistogramTarget, data.FrameTypeTimeSeriesMulti},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frames := metricsFrames(t, tt.target, noBuckets, true)
			require.Len(t, frames, 1)
			require.Empty(t, frames[0].Fields)
			require.Equal(t, tt.frameType, frames[0].Meta.Type)
			require.Equal(t, dataplaneMetricsVersion, frames[0].Meta.TypeVersion)
			require.Equal(t, "A", frames[0].RefID)

			require.Empty(t, metricsFrames(t, tt.target, noBuckets, false))
		})
	}

	t.Run("typed empty series frame reads as no data", func(t *testing.T) {
		collection := requireTimeSeriesMulti(t, metricsFrames(t, termsDateHistogramTarget, noBuckets, true), 0)
		require.True(t, collection.NoData())
	})

	t.Run("date histogram leaf with no buckets is an empty item", func(t *testing.T) {
		target := `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "2" }]}`
		frames := metricsFrames(t, target, noBuckets, true)
		requireTimeSeriesMulti(t, frames, 1)
		require.Equal(t, 0, frames[0].Fields[0].Len())
	})
}

func TestMetricsDataplane_RawDSLAggregationsAreTyped(t *testing.T) {
	stubMetricsDataplane(t, true)
	tests := []struct {
		name, aggs, response string
		check                func(t *testing.T, frames data.Frames)
	}{
		{
			name:     "terms with avg",
			aggs:     `{\"1\":{\"terms\":{\"field\":\"method.keyword\"},\"aggs\":{\"3\":{\"avg\":{\"field\":\"bytes\"}}}}}`,
			response: `{"responses": [{"aggregations": {"1": {"buckets": [{"key": "GET", "doc_count": 10, "3": {"value": 100}}, {"key": "POST", "doc_count": 5, "3": {"value": 50}}]}}}]}`,
			check: func(t *testing.T, frames data.Frames) {
				requireNumericLong(t, frames, 2)
				requireStringAt(t, "GET", fieldByName(t, frames[0], "method.keyword"), 0)
			},
		},
		{
			name:     "date histogram with count",
			aggs:     `{\"2\":{\"date_histogram\":{\"field\":\"@timestamp\",\"fixed_interval\":\"1h\"}}}`,
			response: `{"responses": [{"aggregations": {"2": {"buckets": [{"key": 1000, "doc_count": 1}, {"key": 2000, "doc_count": 2}]}}}]}`,
			check: func(t *testing.T, frames data.Frames) {
				requireTimeSeriesMulti(t, frames, 1)
				require.Equal(t, "Count", frames[0].Fields[1].Name)
			},
		},
		{
			name:     "date histogram then terms",
			aggs:     `{\"2\":{\"date_histogram\":{\"field\":\"@timestamp\",\"fixed_interval\":\"1h\"},\"aggs\":{\"3\":{\"terms\":{\"field\":\"host\"}}}}}`,
			response: `{"responses": [{"aggregations": {"2": {"buckets": [{"key": 1000, "key_as_string": "1970-01-01T00:00:01.000Z", "doc_count": 2, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 1}]}}]}}}]}`,
			check: func(t *testing.T, frames data.Frames) {
				requireTimeSeriesLong(t, frames, 2)
				require.Equal(t, time.UnixMilli(1000).UTC(), fieldByName(t, frames[0], "@timestamp").At(0).(time.Time))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := []byte(`[{"refId": "A", "queryType": "dsl", "metrics": [{ "type": "count", "id": "1" }], "query": "{\"size\": 0, \"aggs\": ` + tt.aggs + `}"}]`)
			result, err := queryDataTest(queries, []byte(tt.response))
			require.NoError(t, err)
			res := result.response.Responses["A"]
			require.NoError(t, res.Error)
			tt.check(t, res.Frames)
		})
	}
}

func TestMetricsDataplane_PipelineWithSingleBucketPathItemName(t *testing.T) {
	target := `{
		"metrics": [
			{ "type": "max", "id": "1", "field": "latency" },
			{ "type": "derivative", "id": "2", "field": "1" },
			{ "type": "derivative", "id": "3", "field": "9" }
		],
		"bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "4" }]
	}`
	response := `{"responses": [{"aggregations": {"4": {"buckets": [
		{"key": 1000, "doc_count": 2, "1": {"value": 5}},
		{"key": 2000, "doc_count": 3, "1": {"value": 7}, "2": {"value": 2}, "3": {"value": 1}}
	]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesMulti(t, frames, 3)
	require.Equal(t, "Max latency", frames[0].Fields[1].Name)
	require.Equal(t, "Derivative Max latency", frames[1].Fields[1].Name)
	require.Equal(t, "Unset", frames[2].Fields[1].Name)
}

func TestMetricsResponse_ExtendedStatsOnTablePath(t *testing.T) {
	target := `{
		"metrics": [
			{ "type": "extended_stats", "id": "1", "field": "bytes", "meta": { "max": true, "std_deviation_bounds_upper": true } },
			{ "type": "count", "id": "2" }
		],
		"bucketAggs": [{ "type": "terms", "field": "host", "id": "3" }]
	}`
	response := `{"responses": [{"aggregations": {"3": {"buckets": [
		{"key": "a", "doc_count": 1, "1": {"max": 10, "std_deviation_bounds": {"upper": 3, "lower": 1}}},
		{"key": "b", "doc_count": 2, "1": {"max": 20, "std_deviation_bounds": {"upper": 4, "lower": 2}}}
	]}}}]}`
	for _, dataplane := range []bool{false, true} {
		frames := metricsFrames(t, target, response, dataplane)
		require.Len(t, frames, 1)
		requireFrameLength(t, frames[0], 2)
		requireFloatAt(t, 20, fieldByName(t, frames[0], "Extended Stats"), 1)
		requireFloatAt(t, 2, fieldByName(t, frames[0], "Count"), 1)
	}
	requireNumericLong(t, metricsFrames(t, target, response, true), 4)
}

func TestMetricsDataplane_OuterDateHistogramIgnoresFormattedKeyAsString(t *testing.T) {
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 1000, "key_as_string": "1970-01-01T00:00:01.000Z", "doc_count": 3, "3": {"buckets": [{"key": "a", "doc_count": 1}, {"key": "b", "doc_count": 2}]}},
		{"key": 2000, "key_as_string": "1970-01-01T00:00:02.000Z", "doc_count": 5, "3": {"buckets": [{"key": "a", "doc_count": 2}, {"key": "b", "doc_count": 3}]}}
	]}}}]}`
	frames := metricsFrames(t, dateHistogramTermsTarget, response, true)
	requireTimeSeriesLong(t, frames, 2)
	require.Equal(t, time.UnixMilli(2000).UTC(), fieldByName(t, frames[0], "@timestamp").At(2).(time.Time))

	// a date_histogram leaf keeps the formatted key as its label
	target := `{
		"metrics": [{ "type": "count", "id": "1" }],
		"bucketAggs": [
			{ "type": "date_histogram", "field": "day", "id": "2" },
			{ "type": "date_histogram", "field": "@timestamp", "id": "3" }
		]
	}`
	nested := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 86400000, "key_as_string": "1970-01-02", "doc_count": 1, "3": {"buckets": [{"key": 86400000, "doc_count": 1}]}}
	]}}}]}`
	frames = metricsFrames(t, target, nested, true)
	requireTimeSeriesMulti(t, frames, 1)
	require.Equal(t, data.Labels{"day": "1970-01-02"}, frames[0].Fields[1].Labels)
}

func TestMetricsDataplane_SeriesKeepLegacyTrimEdgesAndSortDescendingBuckets(t *testing.T) {
	target := `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "2", "settings": { "trimEdges": "1" } }]}`
	response := `{"responses": [{"aggregations": {"2": {"buckets": [
		{"key": 3000, "doc_count": 3}, {"key": 1000, "doc_count": 1}, {"key": 2000, "doc_count": 2}]}}}]}`
	frames := metricsFrames(t, target, response, true)
	requireTimeSeriesMulti(t, frames, 1)
	require.Equal(t, 1, frames[0].Fields[0].Len())
	require.Equal(t, time.UnixMilli(2000).UTC(), frames[0].Fields[0].At(0).(time.Time))
	requireFloatAt(t, 2, frames[0].Fields[1], 0)
}

func TestMetricsDataplane_NestedFiltersStayDistinctDimensions(t *testing.T) {
	filters := func(id string, labels ...string) string {
		items := make([]string, len(labels))
		for i, l := range labels {
			items[i] = `{ "label": "` + l + `", "query": "x:` + l + `" }`
		}
		return `{ "type": "filters", "id": "` + id + `", "settings": { "filters": [` + strings.Join(items, ",") + `] } }`
	}
	t.Run("filters then filters", func(t *testing.T) {
		target := `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [` + filters("2", "a", "b") + `, ` + filters("3", "p", "q") + `]}`
		response := `{"responses": [{"aggregations": {"2": {"buckets": {
			"a": {"doc_count": 3, "3": {"buckets": {"p": {"doc_count": 1}, "q": {"doc_count": 2}}}},
			"b": {"doc_count": 7, "3": {"buckets": {"p": {"doc_count": 3}, "q": {"doc_count": 4}}}}
		}}}}]}`
		frames := metricsFrames(t, target, response, true)
		requireNumericLong(t, frames, 4)
		requireStringAt(t, "a", fieldByName(t, frames[0], "filter"), 0)
		requireStringAt(t, "q", fieldByName(t, frames[0], "filter 3"), 1)

		legacy := metricsFrames(t, target, response, false)
		require.Len(t, legacy[0].Fields, 2)
	})
	t.Run("date histogram then filters then filters", func(t *testing.T) {
		target := `{"metrics": [{ "type": "count", "id": "1" }], "bucketAggs": [{ "type": "date_histogram", "field": "@timestamp", "id": "4" }, ` + filters("2", "a", "b") + `, ` + filters("3", "p", "q") + `]}`
		bucket := func(key string) string {
			return `{"key": ` + key + `, "doc_count": 10, "2": {"buckets": {
				"a": {"doc_count": 3, "3": {"buckets": {"p": {"doc_count": 1}, "q": {"doc_count": 2}}}},
				"b": {"doc_count": 7, "3": {"buckets": {"p": {"doc_count": 3}, "q": {"doc_count": 4}}}}
			}}}`
		}
		response := `{"responses": [{"aggregations": {"4": {"buckets": [` + bucket("1000") + `, ` + bucket("2000") + `]}}}]}`
		requireTimeSeriesLong(t, metricsFrames(t, target, response, true), 4)
	})
}

func TestFormatBucketKey(t *testing.T) {
	tests := []struct {
		key  interface{}
		want string
	}{
		{"a", "a"},
		{float64(100), "100"},
		{float64(0.5), "0.5"},
		{math.Copysign(0, -1), "0"},
		{float64(1234567890123456789), "1234567890123456768"},
		{float64(9223372036854775807), "9223372036854775808"},
		{float64(1000000), "1000000"},
	}
	for _, tt := range tests {
		got, ok := formatBucketKey(simplejson.NewFromAny(map[string]interface{}{"key": tt.key}))
		require.True(t, ok)
		require.Equal(t, tt.want, got)
	}
	// Elasticsearch sends boolean keys as 1/0 with key_as_string; a bare JSON bool is not a key
	for _, key := range []interface{}{true, []interface{}{1}, nil} {
		_, ok := formatBucketKey(simplejson.NewFromAny(map[string]interface{}{"key": key}))
		require.False(t, ok)
	}
}
