package elasticsearch

import (
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func esqlTarget(query string) *Query {
	return &Query{RefID: "A", RawQuery: query, Metrics: []*MetricAgg{{Type: countType}}}
}

func TestEsqlDataplane_EveryAggregateBecomesASeries(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "MAX(cpu)", Type: "double"},
			{Name: "MIN(cpu)", Type: "double"},
			{Name: "BUCKET(@timestamp, 10 minutes)", Type: "date"},
		},
		Values: [][]interface{}{
			{0.9, 0.1, "2026-02-04T00:00:00.000Z"},
			{0.8, 0.2, "2026-02-04T00:10:00.000Z"},
		},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM metrics-* | STATS MAX(cpu), MIN(cpu) BY BUCKET(@timestamp, 10 minutes)"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	require.Equal(t, "MAX(cpu)", res.Frames[0].Fields[1].Name)
	require.Equal(t, "MIN(cpu)", res.Frames[1].Fields[1].Name)
	require.Equal(t, "MAX(cpu)", res.Frames[0].Name)
	require.Nil(t, res.Frames[0].Fields[1].Labels)
	require.Equal(t, 2, res.Frames[0].Fields[0].Len())
	requireFloatAt(t, 0.2, res.Frames[1].Fields[1], 1)
}

func TestEsqlDataplane_NumericByKeysAreDimensionsAndRowsAreSorted(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "c", Type: "long"},
			{Name: "BUCKET(@timestamp, 1h)", Type: "date"},
			{Name: "status_code", Type: "integer"},
		},
		Values: [][]interface{}{
			{float64(3), "2026-02-04T01:00:00.000Z", float64(404)},
			{float64(10), "2026-02-04T00:00:00.000Z", float64(200)},
			{float64(2), "2026-02-04T00:00:00.000Z", float64(404)},
			{float64(12), "2026-02-04T01:00:00.000Z", float64(200)},
		},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM logs-* | STATS c = COUNT(*) BY BUCKET(@timestamp, 1h), status_code"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	// groups appear in first-seen order and rows inside each are sorted by time
	require.Equal(t, data.Labels{"status_code": "404"}, res.Frames[0].Fields[1].Labels)
	require.Equal(t, time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC), res.Frames[0].Fields[0].At(0).(time.Time))
	requireFloatAt(t, 2, res.Frames[0].Fields[1], 0)
	requireFloatAt(t, 3, res.Frames[0].Fields[1], 1)
	require.Equal(t, "c", res.Frames[0].Fields[1].Name)
	require.Equal(t, "404", res.Frames[0].Name)
	require.Equal(t, "404", res.Frames[0].Fields[1].Config.DisplayNameFromDS)
}

func TestEsqlDataplane_LabelValueFormatting(t *testing.T) {
	require.Equal(t, "1000000", esqlLabelValue(float64(1000000)))
	require.Equal(t, "1.5", esqlLabelValue(1.5))
	require.Equal(t, "true", esqlLabelValue(true))
	require.Equal(t, "a,b", esqlLabelValue([]interface{}{"a", "b"}))
	require.Equal(t, "", esqlLabelValue(nil))
}

func TestEsqlDataplane_NoRowsIsATypedEmptyFrame(t *testing.T) {
	query := "FROM logs-* | STATS c = COUNT(*) BY BUCKET(@timestamp, 1h)"
	response := &es.EsqlResponse{Columns: []es.EsqlColumn{{Name: "c", Type: "long"}, {Name: "BUCKET(@timestamp, 1h)", Type: "date"}}}
	res, err := processEsqlMetricsResponse(response, esqlTarget(query), true)
	require.NoError(t, err)
	collection := requireTimeSeriesMulti(t, res.Frames, 0)
	require.True(t, collection.NoData())
	require.Equal(t, "A", res.Frames[0].RefID)

	res, err = processEsqlMetricsResponse(nil, esqlTarget(query), true)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Empty(t, res.Frames[0].Fields)
	require.Equal(t, data.FrameTypeTimeSeriesMulti, res.Frames[0].Meta.Type)
}

func TestEsqlDataplane_GroupedWithoutTimeIsNumericLong(t *testing.T) {
	query := "FROM logs-* | STATS c = COUNT(*) BY host"
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "c", Type: "long"}, {Name: "host", Type: "keyword"}},
		Values:  [][]interface{}{{float64(5), "a"}, {float64(7), "b"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget(query), true)
	require.NoError(t, err)
	requireNumericLong(t, res.Frames, 2)
	require.Equal(t, "c", res.Frames[0].Fields[0].Name)
	require.Equal(t, data.FieldTypeString, res.Frames[0].Fields[1].Type())
	require.Equal(t, "a", res.Frames[0].Fields[1].At(0))

	res, err = processEsqlMetricsResponse(&es.EsqlResponse{Columns: response.Columns}, esqlTarget(query), true)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, data.FrameTypeNumericLong, res.Frames[0].Meta.Type)
	require.Empty(t, res.Frames[0].Fields)
}

func TestEsqlDataplane_UnmatchedKeysAreClassifiedByType(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "c", Type: "long"},
			{Name: "BUCKET(@timestamp, 1h)", Type: "date"},
			{Name: "h", Type: "keyword"},
		},
		Values: [][]interface{}{
			{float64(1), "2026-02-04T00:00:00.000Z", "a"},
			{float64(2), "2026-02-04T00:00:00.000Z", "b"},
		},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM logs-* | STATS c = COUNT(*) BY BUCKET(@timestamp, 1h), host | RENAME host AS h"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	require.Equal(t, data.Labels{"h": "a"}, res.Frames[0].Fields[1].Labels)
	require.Equal(t, "c", res.Frames[0].Fields[1].Name)
}

func TestEsqlDataplane_AliasedBucketKeyIsTheTimeColumn(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "last", Type: "date"},
			{Name: "c", Type: "long"},
			{Name: "t", Type: "date"},
		},
		Values: [][]interface{}{{"2026-02-04T00:59:00.000Z", float64(1), "2026-02-04T00:00:00.000Z"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM logs-* | STATS last = MAX(@timestamp), c = COUNT(*) BY t = BUCKET(@timestamp, 1h)"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 1)
	require.Equal(t, time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC), res.Frames[0].Fields[0].At(0).(time.Time))
	// a non-numeric aggregate rides along as a remainder field, not a dimension
	require.Nil(t, res.Frames[0].Fields[1].Labels)
	require.Len(t, res.Frames[0].Fields, 3)
	require.Equal(t, "last", res.Frames[0].Fields[2].Name)
	require.Equal(t, "2026-02-04T00:59:00.000Z", res.Frames[0].Fields[2].At(0))
}

func TestEsqlDataplane_PromqlResultIsTypedAndSorted(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "avg(metrics.system.cpu.logical.count)", Type: "double"},
			{Name: "step", Type: "date"},
			{Name: "host.name", Type: "keyword"},
		},
		Values: [][]interface{}{
			{6.6, "2026-05-21T14:01:00.000Z", "a"},
			{6.2, "2026-05-21T14:00:00.000Z", "a"},
			{5.8, "2026-05-21T14:00:00.000Z", "b"},
		},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("PROMQL index=metrics-* step=1m avg(metrics.system.cpu.logical.count)"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	require.Equal(t, "avg(metrics.system.cpu.logical.count)", res.Frames[0].Fields[1].Name)
	require.Equal(t, "a", res.Frames[0].Name)
	requireFloatAt(t, 6.2, res.Frames[0].Fields[1], 0)
}

func TestEsqlDataplane_CounterAndUnsignedTypesAreValues(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "MAX(requests)", Type: "counter_long"},
			{Name: "MAX(bytes)", Type: "unsigned_long"},
			{Name: "BUCKET(@timestamp, 1h)", Type: "date"},
		},
		Values: [][]interface{}{{float64(1), float64(2), "2026-02-04T00:00:00.000Z"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("TS metrics-* | STATS MAX(requests), MAX(bytes) BY BUCKET(@timestamp, 1h)"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
}

func TestEsqlDataplane_NoNumericColumnStaysATable(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "VALUES(host)", Type: "keyword"}, {Name: "BUCKET(@timestamp, 1h)", Type: "date"}},
		Values:  [][]interface{}{{"a", "2026-02-04T00:00:00.000Z"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM logs-* | STATS VALUES(host) BY BUCKET(@timestamp, 1h)"), true)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.EqualValues(t, data.VisTypeTable, res.Frames[0].Meta.PreferredVisualization)
	require.Equal(t, data.FrameTypeUnknown, res.Frames[0].Meta.Type)
}

func TestEsqlDataplane_MultiValueLegendNamesTheColumn(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "MAX(cpu)", Type: "double"},
			{Name: "MIN(cpu)", Type: "double"},
			{Name: "BUCKET(@timestamp, 1h)", Type: "date"},
			{Name: "host", Type: "keyword"},
		},
		Values: [][]interface{}{{0.9, 0.1, "2026-02-04T00:00:00.000Z", "a"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM m | STATS MAX(cpu), MIN(cpu) BY BUCKET(@timestamp, 1h), host"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	require.Equal(t, "a MAX(cpu)", res.Frames[0].Fields[1].Config.DisplayNameFromDS)
	require.Equal(t, "a MIN(cpu)", res.Frames[1].Fields[1].Config.DisplayNameFromDS)
}

func TestEsqlStatsByKeys(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"FROM a | STATS c = COUNT(*) BY host", []string{"host"}},
		{"FROM a | STATS c = COUNT(*) BY BUCKET(@timestamp, 1h), host.name", []string{"BUCKET(@timestamp, 1h)", "host.name"}},
		{"FROM a | stats count(*) by t = bucket(@timestamp, 1 hour), `host.name`", []string{"t", "host.name"}},
		{"FROM a | WHERE msg == \"BY | x, y\" | STATS c = COUNT(*) BY CONCAT(a, \",\", b), region", []string{"CONCAT(a, \",\", b)", "region"}},
		{"FROM a | STATS c = COUNT(*) BY host | STATS m = MAX(c) BY region", []string{"region"}},
		{"FROM a | STATS c = COUNT(*) WHERE x > 1 BY host", []string{"host"}},
		{"FROM a | STATS c = COUNT(*) BY host == \"a\"", []string{"host == \"a\""}},
		{"FROM a\n| STATS c = COUNT(*)\n    BY host,\n       region", []string{"host", "region"}},
		{`FROM a | STATS c = COUNT(*) WHERE msg == "x BY y" BY host`, []string{"host"}},
		{"FROM a | STATS c = COUNT(*) WHERE `BY x` == 1 BY host", []string{"host"}},
		{`FROM a | STATS c = COUNT(*) BY CASE(x > 1, "BY", "no")`, []string{`CASE(x > 1, "BY", "no")`}},
		{"FROM a | STATS c = COUNT(*) BY host,", []string{"host"}},
		{"FROM a | STATS c = COUNT(*)", nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			keys, hasStats := esqlStatsByKeys(tt.query)
			require.True(t, hasStats)
			require.Equal(t, tt.want, keys)
		})
	}
	keys, hasStats := esqlStatsByKeys("FROM a | LIMIT 10")
	require.False(t, hasStats)
	require.Nil(t, keys)
}

func TestEsqlDataplane_StatsWithoutByMakesEveryAggregateAValue(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "total", Type: "long"}, {Name: "n", Type: "long"}, {Name: "last", Type: "date"}},
		Values:  [][]interface{}{{float64(10), float64(2), "2026-02-04T00:00:00.000Z"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM a | STATS total = SUM(x), n = COUNT(*), last = MAX(@timestamp)"), true)
	require.NoError(t, err)
	collection := requireNumericLong(t, res.Frames, 2)
	require.Equal(t, data.FieldTypeNullableFloat64, res.Frames[0].Fields[1].Type())
	require.Equal(t, data.FieldTypeString, res.Frames[0].Fields[2].Type())
	require.Equal(t, "total", collection.Refs[0].GetMetricName())
}

func TestEsqlDataplane_UnparseableTimestampRowsAreDropped(t *testing.T) {
	query := "FROM a | STATS c = COUNT(*) BY BUCKET(@timestamp, 1h)"
	columns := []es.EsqlColumn{{Name: "c", Type: "long"}, {Name: "BUCKET(@timestamp, 1h)", Type: "date"}}
	res, err := processEsqlMetricsResponse(&es.EsqlResponse{Columns: columns, Values: [][]interface{}{{float64(1), "garbage"}, {float64(2), "2026-02-04T00:00:00.000Z"}}}, esqlTarget(query), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 1)
	require.Equal(t, 1, res.Frames[0].Fields[0].Len())

	res, err = processEsqlMetricsResponse(&es.EsqlResponse{Columns: columns, Values: [][]interface{}{{float64(1), "garbage"}}}, esqlTarget(query), true)
	require.NoError(t, err)
	require.True(t, requireTimeSeriesMulti(t, res.Frames, 0).NoData())
}

func TestEsqlDataplane_FallbackUsesLastDateColumnWhenBucketIsRenamed(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "c", Type: "long"}, {Name: "ts", Type: "date"}, {Name: "host", Type: "keyword"}},
		Values:  [][]interface{}{{float64(1), "2026-02-04T00:00:00.000Z", "a"}},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM a | STATS c = COUNT(*) BY t = BUCKET(@timestamp, 1h), host | RENAME t AS ts"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 1)
	require.Equal(t, data.Labels{"host": "a"}, res.Frames[0].Fields[1].Labels)

	// a date aggregate precedes the renamed bucket key: BY keys trail aggregates, so the last date column is time
	response = &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "last", Type: "date"}, {Name: "c", Type: "long"}, {Name: "ts", Type: "date"}, {Name: "host", Type: "keyword"}},
		Values: [][]interface{}{
			{"2026-02-04T00:59:00.000Z", float64(1), "2026-02-04T00:00:00.000Z", "a"},
			{"2026-02-04T01:59:00.000Z", float64(2), "2026-02-04T01:00:00.000Z", "a"},
		},
	}
	res, err = processEsqlMetricsResponse(response, esqlTarget("FROM a | STATS last = MAX(@timestamp), c = COUNT(*) BY t = BUCKET(@timestamp, 1h), host | RENAME t AS ts"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 1)
	require.Equal(t, time.Date(2026, 2, 4, 1, 0, 0, 0, time.UTC), res.Frames[0].Fields[0].At(1).(time.Time))
	require.Equal(t, data.Labels{"host": "a"}, res.Frames[0].Fields[1].Labels)
}

func TestEsqlStatsClauses(t *testing.T) {
	aggs, keys, hasStats := esqlStatsClauses("FROM a | STATS c = COUNT(*) WHERE x > 1, MAX(cpu), `my agg` = AVG(y) BY BUCKET(@timestamp, 1h), host")
	require.True(t, hasStats)
	require.Equal(t, []string{"c", "MAX(cpu)", "my agg"}, aggs)
	require.Equal(t, []string{"BUCKET(@timestamp, 1h)", "host"}, keys)

	aggs, keys, hasStats = esqlStatsClauses("FROM a | STATS total = SUM(x), n = COUNT(*)")
	require.True(t, hasStats)
	require.Equal(t, []string{"total", "n"}, aggs)
	require.Nil(t, keys)
}

func TestEsqlDataplane_NullCells(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{{Name: "MAX(cpu)", Type: "double"}, {Name: "BUCKET(@timestamp, 1h)", Type: "date"}, {Name: "host", Type: "keyword"}},
		Values: [][]interface{}{
			{0.5, "2026-02-04T00:00:00.000Z", "a"},
			{nil, "2026-02-04T01:00:00.000Z", "a"},
			{0.7, "2026-02-04T00:00:00.000Z", nil},
		},
	}
	res, err := processEsqlMetricsResponse(response, esqlTarget("FROM a | STATS MAX(cpu) BY BUCKET(@timestamp, 1h), host"), true)
	require.NoError(t, err)
	requireTimeSeriesMulti(t, res.Frames, 2)
	require.Equal(t, 2, res.Frames[0].Fields[1].Len())
	require.Nil(t, res.Frames[0].Fields[1].At(1).(*float64))
	require.Equal(t, data.Labels{"host": ""}, res.Frames[1].Fields[1].Labels)
}
