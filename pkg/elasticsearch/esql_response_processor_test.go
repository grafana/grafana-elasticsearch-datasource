package elasticsearch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func TestProcessEsqlMetricsResponse_ReturnsTimeSeriesForCountMetric(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "count(*)", Type: "long"},
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
		},
		Values: [][]interface{}{
			{int64(41679), "2026-02-04T00:00:00.000Z"},
			{int64(83152), "2026-02-05T00:00:00.000Z"},
			{int64(41568), "2026-02-09T00:00:00.000Z"},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*) by BUCKET(@timestamp, 10, \"2026-02-02T18:00:46.258Z\", \"2026-02-09T18:00:46.258Z\")",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	frame := res.Frames[0]
	require.Equal(t, "Count", frame.Name)
	require.NotNil(t, frame.Meta)
	require.Equal(t, data.FrameTypeTimeSeriesMulti, frame.Meta.Type)
	require.Len(t, frame.Fields, 2)
	require.Equal(t, data.TimeSeriesTimeFieldName, frame.Fields[0].Name)
	require.Equal(t, data.TimeSeriesValueFieldName, frame.Fields[1].Name)

	require.Equal(t, 3, frame.Fields[0].Len())
	require.Equal(t, 3, frame.Fields[1].Len())

	ts1, ok := frame.Fields[0].At(0).(time.Time)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC), ts1)

	v1, ok := frame.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.NotNil(t, v1)
	require.Equal(t, 41679.0, *v1)
}

func TestProcessEsqlMetricsResponse_FallsBackToTableWhenNoTimeColumn(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "count(*)", Type: "long"},
		},
		Values: [][]interface{}{
			{float64(10)},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*)",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	frame := res.Frames[0]
	require.NotNil(t, frame.Meta)
	require.Equal(t, data.VisType(data.VisTypeTable), frame.Meta.PreferredVisualization)
}

func TestProcessEsqlMetricsResponse_GroupsByBreakdownFields(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(metrics.system.memory.utilization)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", "host-a", 0.528},
			{"2026-02-04T00:00:00.000Z", "host-b", 0.566},
			{"2026-02-04T00:10:00.000Z", "host-a", 0.510},
			{"2026-02-04T00:10:00.000Z", "host-b", 0.485},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(metrics.system.memory.utilization) BY BUCKET(@timestamp, 10 minutes), host.name",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2, "should create one frame per unique host.name")

	// Frame for host-a
	frameA := res.Frames[0]
	require.Equal(t, data.FrameTypeTimeSeriesMulti, frameA.Meta.Type)
	require.Equal(t, 2, frameA.Fields[0].Len())
	require.Equal(t, "host-a", frameA.Fields[1].Labels["host.name"])

	v0, ok := frameA.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.528, *v0)

	v1, ok := frameA.Fields[1].At(1).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.510, *v1)

	// Frame for host-b
	frameB := res.Frames[1]
	require.Equal(t, "host-b", frameB.Fields[1].Labels["host.name"])

	vb0, ok := frameB.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.566, *vb0)

	vb1, ok := frameB.Fields[1].At(1).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.485, *vb1)
}

func TestProcessEsqlMetricsResponse_MultipleBreakdownFields(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "region", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", "host-a", "us-east", 0.5},
			{"2026-02-04T00:00:00.000Z", "host-a", "us-west", 0.6},
			{"2026-02-04T00:10:00.000Z", "host-a", "us-east", 0.7},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(cpu) BY BUCKET(@timestamp, 10 minutes), host.name, region",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2, "should create one frame per unique (host.name, region) combination")

	// Frame for host-a / us-east
	frame0 := res.Frames[0]
	require.Equal(t, "host-a", frame0.Fields[1].Labels["host.name"])
	require.Equal(t, "us-east", frame0.Fields[1].Labels["region"])
	require.Equal(t, 2, frame0.Fields[0].Len())

	// Frame for host-a / us-west
	frame1 := res.Frames[1]
	require.Equal(t, "host-a", frame1.Fields[1].Labels["host.name"])
	require.Equal(t, "us-west", frame1.Fields[1].Labels["region"])
	require.Equal(t, 1, frame1.Fields[0].Len())
}

func TestProcessEsqlMetricsResponse_NilResponse(t *testing.T) {
	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*) BY BUCKET(@timestamp, 1 day)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(nil, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, "A", res.Frames[0].Name)
	require.Len(t, res.Frames[0].Fields, 0)
}

func TestProcessEsqlMetricsResponse_EmptyColumns(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{},
		Values:  [][]interface{}{},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*) BY BUCKET(@timestamp, 1 day)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, "A", res.Frames[0].Name)
}

func TestProcessEsqlMetricsResponse_NoValueColumnFallsBackToTable(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", "host-a"},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*) BY BUCKET(@timestamp, 1 day), host.name",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, data.VisType(data.VisTypeTable), res.Frames[0].Meta.PreferredVisualization)
}

func TestProcessEsqlMetricsResponse_NilBreakdownValues(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", nil, 0.5},
			{"2026-02-04T00:00:00.000Z", "host-b", 0.6},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(cpu) BY BUCKET(@timestamp, 10 minutes), host.name",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2)

	// Nil breakdown value becomes empty string label
	require.Equal(t, "", res.Frames[0].Fields[1].Labels["host.name"])
	require.Equal(t, "host-b", res.Frames[1].Fields[1].Labels["host.name"])
}

func TestProcessEsqlMetricsResponse_NilMetricValue(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", "host-a", nil},
			{"2026-02-04T00:10:00.000Z", "host-a", 0.7},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(cpu) BY BUCKET(@timestamp, 10 minutes), host.name",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	// First value should be nil, second should have data
	require.Nil(t, res.Frames[0].Fields[1].At(0))

	v1, ok := res.Frames[0].Fields[1].At(1).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.7, *v1)
}

func TestProcessEsqlMetricsResponse_UnparseableTimestampsFallBackToTable(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "count(*)", Type: "long"},
		},
		Values: [][]interface{}{
			{"not-a-date", int64(10)},
			{"also-not-a-date", int64(20)},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | STATS count(*) BY BUCKET(@timestamp, 1 day)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, data.VisType(data.VisTypeTable), res.Frames[0].Meta.PreferredVisualization)
}

func TestProcessEsqlMetricsResponse_BreakdownWithUnparseableTimestampsFallBackToTable(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"not-a-date", "host-a", 0.5},
			{"also-not-a-date", "host-b", 0.6},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(cpu) BY BUCKET(@timestamp, 10 minutes), host.name",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	require.Equal(t, data.VisType(data.VisTypeTable), res.Frames[0].Meta.PreferredVisualization)
}

func TestProcessEsqlMetricsResponse_PicksFirstNumericColumn(t *testing.T) {
	// When there are two numeric columns, the first is the value column.
	// The second numeric column is currently treated as a breakdown column,
	// which groups rows by its stringified value.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "MAX(cpu)", Type: "double"},
			{Name: "MIN(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", 0.9, 0.1},
			{"2026-02-04T00:10:00.000Z", 0.8, 0.2},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM metrics-* | STATS MAX(cpu), MIN(cpu) BY BUCKET(@timestamp, 10 minutes)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	// Two frames because each row has a unique MIN(cpu) value treated as breakdown
	require.Len(t, res.Frames, 2)

	// Values come from the first numeric column (MAX(cpu))
	v0, ok := res.Frames[0].Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.9, *v0)

	v1, ok := res.Frames[1].Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 0.8, *v1)
}

// --- Tests for classifyEsqlColumns ---

func TestClassifyEsqlColumns_TimeValueAndBreakdown(t *testing.T) {
	columns := []es.EsqlColumn{
		{Name: "BUCKET(@timestamp, ...)", Type: "date"},
		{Name: "host.name", Type: "keyword"},
		{Name: "MAX(cpu)", Type: "double"},
	}

	layout := classifyEsqlColumns(columns)
	require.Equal(t, 0, layout.timeColIdx)
	require.Equal(t, 2, layout.valueColIdx)
	require.Equal(t, []int{1}, layout.breakdownColIdxs)
}

func TestClassifyEsqlColumns_TimeAndValueOnly(t *testing.T) {
	columns := []es.EsqlColumn{
		{Name: "count(*)", Type: "long"},
		{Name: "BUCKET(@timestamp, ...)", Type: "date"},
	}

	layout := classifyEsqlColumns(columns)
	require.Equal(t, 1, layout.timeColIdx)
	require.Equal(t, 0, layout.valueColIdx)
	require.Empty(t, layout.breakdownColIdxs)
}

func TestClassifyEsqlColumns_NoTimeColumn(t *testing.T) {
	columns := []es.EsqlColumn{
		{Name: "count(*)", Type: "long"},
		{Name: "host.name", Type: "keyword"},
	}

	layout := classifyEsqlColumns(columns)
	require.Equal(t, -1, layout.timeColIdx)
	require.Equal(t, 0, layout.valueColIdx)
}

func TestClassifyEsqlColumns_NoValueColumn(t *testing.T) {
	columns := []es.EsqlColumn{
		{Name: "BUCKET(@timestamp, ...)", Type: "date"},
		{Name: "host.name", Type: "keyword"},
	}

	layout := classifyEsqlColumns(columns)
	require.Equal(t, 0, layout.timeColIdx)
	require.Equal(t, -1, layout.valueColIdx)
}

func TestClassifyEsqlColumns_MultipleBreakdowns(t *testing.T) {
	columns := []es.EsqlColumn{
		{Name: "BUCKET(@timestamp, ...)", Type: "date"},
		{Name: "host.name", Type: "keyword"},
		{Name: "region", Type: "keyword"},
		{Name: "MAX(cpu)", Type: "double"},
	}

	layout := classifyEsqlColumns(columns)
	require.Equal(t, 0, layout.timeColIdx)
	require.Equal(t, 3, layout.valueColIdx)
	require.Equal(t, []int{1, 2}, layout.breakdownColIdxs)
}

// --- Tests for buildEsqlSingleSeriesFrame ---

func TestBuildEsqlSingleSeriesFrame_ValidRows(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "count(*)", Type: "long"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", int64(10)},
			{"2026-02-05T00:00:00.000Z", int64(20)},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 1}

	frame := buildEsqlSingleSeriesFrame(response, layout, "Count")
	require.NotNil(t, frame)
	require.Equal(t, "Count", frame.Name)
	require.Equal(t, 2, frame.Fields[0].Len())
}

func TestBuildEsqlSingleSeriesFrame_ReturnsNilWhenNoParseableTimestamps(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "count(*)", Type: "long"},
		},
		Values: [][]interface{}{
			{"not-a-date", int64(10)},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 1}

	frame := buildEsqlSingleSeriesFrame(response, layout, "Count")
	require.Nil(t, frame)
}

func TestBuildEsqlSingleSeriesFrame_NilMetricValue(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", nil},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 1}

	frame := buildEsqlSingleSeriesFrame(response, layout, "Count")
	require.NotNil(t, frame)
	require.Equal(t, 1, frame.Fields[0].Len())
	require.Nil(t, frame.Fields[1].At(0))
}

// --- Tests for buildEsqlMultiSeriesFrames ---

func TestBuildEsqlMultiSeriesFrames_GroupsByBreakdown(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", "host-a", 0.5},
			{"2026-02-04T00:00:00.000Z", "host-b", 0.6},
			{"2026-02-04T00:10:00.000Z", "host-a", 0.7},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 2, breakdownColIdxs: []int{1}}

	frames := buildEsqlMultiSeriesFrames(response, layout, "Count")
	require.Len(t, frames, 2)
	require.Equal(t, "host-a", frames[0].Fields[1].Labels["host.name"])
	require.Equal(t, 2, frames[0].Fields[0].Len())
	require.Equal(t, "host-b", frames[1].Fields[1].Labels["host.name"])
	require.Equal(t, 1, frames[1].Fields[0].Len())
}

func TestBuildEsqlMultiSeriesFrames_ReturnsNilWhenNoParseableTimestamps(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"not-a-date", "host-a", 0.5},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 2, breakdownColIdxs: []int{1}}

	frames := buildEsqlMultiSeriesFrames(response, layout, "Count")
	require.Nil(t, frames)
}

func TestBuildEsqlMultiSeriesFrames_NilBreakdownAndMetricValues(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "BUCKET(@timestamp, ...)", Type: "date"},
			{Name: "host.name", Type: "keyword"},
			{Name: "MAX(cpu)", Type: "double"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", nil, nil},
			{"2026-02-04T00:00:00.000Z", "host-b", 0.6},
		},
	}
	layout := esqlColumnLayout{timeColIdx: 0, valueColIdx: 2, breakdownColIdxs: []int{1}}

	frames := buildEsqlMultiSeriesFrames(response, layout, "Count")
	require.Len(t, frames, 2)
	require.Equal(t, "", frames[0].Fields[1].Labels["host.name"])
	require.Nil(t, frames[0].Fields[1].At(0))
	require.Equal(t, "host-b", frames[1].Fields[1].Labels["host.name"])
}

func TestProcessEsqlMetricsResponse_ReturnsEmptySuccessWhenNoStatsCommand(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "@timestamp", Type: "date"},
			{Name: "bytes", Type: "long"},
		},
		Values: [][]interface{}{
			{"2026-02-04T00:00:00.000Z", int64(10)},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "FROM logs* | LIMIT 10",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Empty(t, res.Frames)
}

func TestProcessEsqlMetricsResponse_ReturnsTimeSeriesForPromqlQuery(t *testing.T) {
	// PROMQL responses carry a numeric value column plus a `step` date column,
	// even though they never contain a STATS command.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "avg(metrics.system.cpu.logical.count)", Type: "double"},
			{Name: "step", Type: "date"},
		},
		Values: [][]interface{}{
			{6.2, "2026-05-21T14:00:00.000Z"},
			{6.6, "2026-05-21T14:01:00.000Z"},
			{5.8, "2026-05-21T14:02:00.000Z"},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "PROMQL index=metrics-* step=1m avg(metrics.system.cpu.logical.count)",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	frame := res.Frames[0]
	require.NotNil(t, frame.Meta)
	require.Equal(t, data.FrameTypeTimeSeriesMulti, frame.Meta.Type)
	require.Len(t, frame.Fields, 2)
	require.Equal(t, data.TimeSeriesTimeFieldName, frame.Fields[0].Name)
	require.Equal(t, data.TimeSeriesValueFieldName, frame.Fields[1].Name)

	require.Equal(t, 3, frame.Fields[0].Len())
	require.Equal(t, 3, frame.Fields[1].Len())

	ts1, ok := frame.Fields[0].At(0).(time.Time)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 5, 21, 14, 0, 0, 0, time.UTC), ts1)

	v1, ok := frame.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.NotNil(t, v1)
	require.Equal(t, 6.2, *v1)
}

func TestProcessEsqlMetricsResponse_DetectsPromqlBehindLeadingComment(t *testing.T) {
	// A leading ES|QL comment must not stop PROMQL detection, otherwise the
	// query silently returns no data even though Elasticsearch executed it.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "avg(metrics.system.cpu.logical.count)", Type: "double"},
			{Name: "step", Type: "date"},
		},
		Values: [][]interface{}{
			{6.2, "2026-05-21T14:00:00.000Z"},
			{6.6, "2026-05-21T14:01:00.000Z"},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "// cpu usage\nPROMQL index=metrics-* step=1m avg(metrics.system.cpu.logical.count)",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	frame := res.Frames[0]
	require.NotNil(t, frame.Meta)
	require.Equal(t, data.FrameTypeTimeSeriesMulti, frame.Meta.Type)
	require.Equal(t, 2, frame.Fields[0].Len())
}

func TestHasEsqlMetricsCommand(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{
			name:  "plain PROMQL query",
			query: "PROMQL index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "lowercase promql query",
			query: "promql index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "PROMQL behind leading line comment",
			query: "// cpu usage\nPROMQL index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "lowercase promql behind leading line comment",
			query: "// c\npromql index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "PROMQL behind leading block comment",
			query: "/* cpu usage */ PROMQL index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "PROMQL behind multiple stacked leading comments",
			query: "// first\n/* second */\n// third\nPROMQL index=metrics-* step=1m avg(cpu)",
			want:  true,
		},
		{
			name:  "comment-only query",
			query: "// just a comment",
			want:  false,
		},
		{
			name:  "unterminated block comment",
			query: "/* unterminated PROMQL index=metrics-*",
			want:  false,
		},
		{
			name:  "STATS query",
			query: "FROM x | STATS count(*) BY BUCKET(@timestamp, 1 day)",
			want:  true,
		},
		{
			name:  "interior comment marker inside a string literal",
			query: "FROM x | WHERE url == \"http://a\"",
			want:  false,
		},
		{
			name:  "PROMQL as a non-first command is not a source command",
			query: "FROM x | PROMQL avg(cpu)",
			want:  false,
		},
		{
			name:  "empty query",
			query: "",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hasEsqlMetricsCommand(tc.query))
		})
	}
}

func TestProcessEsqlMetricsResponse_GroupsByBreakdownFieldsForPromqlQuery(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "avg(metrics.system.cpu.logical.count)", Type: "double"},
			{Name: "step", Type: "date"},
			{Name: "host.name", Type: "keyword"},
		},
		Values: [][]interface{}{
			{6.2, "2026-05-21T14:00:00.000Z", "host-a"},
			{4.5, "2026-05-21T14:00:00.000Z", "host-b"},
			{6.6, "2026-05-21T14:01:00.000Z", "host-a"},
			{5.0, "2026-05-21T14:01:00.000Z", "host-b"},
		},
	}

	target := &Query{
		RefID:    "A",
		RawQuery: "PROMQL index=metrics-* step=1m avg by (host.name) (metrics.system.cpu.logical.count)",
		Metrics: []*MetricAgg{
			{Type: countType},
		},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2, "should create one frame per unique host.name")

	frameA := res.Frames[0]
	require.Equal(t, data.FrameTypeTimeSeriesMulti, frameA.Meta.Type)
	require.Equal(t, 2, frameA.Fields[0].Len())
	require.Equal(t, "host-a", frameA.Fields[1].Labels["host.name"])

	va0, ok := frameA.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 6.2, *va0)

	frameB := res.Frames[1]
	require.Equal(t, "host-b", frameB.Fields[1].Labels["host.name"])

	vb0, ok := frameB.Fields[1].At(0).(*float64)
	require.True(t, ok)
	require.Equal(t, 4.5, *vb0)
}

func concreteAt(t *testing.T, field *data.Field, idx int) any {
	t.Helper()
	v, ok := field.ConcreteAt(idx)
	require.Truef(t, ok, "field %q row %d is nil", field.Name, idx)
	return v
}

func TestProcessEsqlRawDataResponse_NumericColumnTypes(t *testing.T) {
	// Values are json.Number, as the ES|QL decoder delivers them. The
	// unsigned_long and long values sit above 2^53, where float64 rounds.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "job_id", Type: "unsigned_long"},
			{Name: "big_long", Type: "long"},
			{Name: "requests", Type: "counter_long"},
			{Name: "errors", Type: "counter_integer"},
			{Name: "load", Type: "counter_double"},
			{Name: "ratio", Type: "double"},
			{Name: "name", Type: "keyword"},
		},
		Values: [][]any{
			{json.Number("18446744073709551615"), json.Number("9007199254740993"), json.Number("12345678901"), json.Number("1234567"), json.Number("1234567.5"), json.Number("1.0E21"), "a"},
			{json.Number("1234567"), json.Number("-9007199254740993"), nil, nil, nil, json.Number("1.5"), "b"},
		},
	}

	res, err := processEsqlRawDataResponse(response, &Query{RefID: "A"})
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)
	frame := res.Frames[0]
	require.Len(t, frame.Fields, 7)

	jobID := frame.Fields[0]
	require.Equal(t, data.FieldTypeNullableUint64, jobID.Type())
	require.Equal(t, uint64(18446744073709551615), concreteAt(t, jobID, 0))
	require.Equal(t, uint64(1234567), concreteAt(t, jobID, 1))

	bigLong := frame.Fields[1]
	require.Equal(t, data.FieldTypeNullableInt64, bigLong.Type())
	require.Equal(t, int64(9007199254740993), concreteAt(t, bigLong, 0))
	require.Equal(t, int64(-9007199254740993), concreteAt(t, bigLong, 1))

	requests := frame.Fields[2]
	require.Equal(t, data.FieldTypeNullableInt64, requests.Type())
	require.Equal(t, int64(12345678901), concreteAt(t, requests, 0))
	_, ok := requests.ConcreteAt(1)
	require.False(t, ok)

	errors := frame.Fields[3]
	require.Equal(t, data.FieldTypeNullableInt64, errors.Type())
	require.Equal(t, int64(1234567), concreteAt(t, errors, 0))

	load := frame.Fields[4]
	require.Equal(t, data.FieldTypeNullableFloat64, load.Type())
	require.Equal(t, 1234567.5, concreteAt(t, load, 0))

	ratio := frame.Fields[5]
	require.Equal(t, data.FieldTypeNullableFloat64, ratio.Type())
	require.Equal(t, 1e21, concreteAt(t, ratio, 0))

	name := frame.Fields[6]
	require.Equal(t, data.FieldTypeNullableString, name.Type())
	require.Equal(t, "a", concreteAt(t, name, 0))
}

func TestToString_FormatsNumbersWithoutExponent(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{"float64 fixture", float64(1234567), "1234567"},
		{"unsigned_long digits", json.Number("18446744073709551615"), "18446744073709551615"},
		{"integral double keeps the integer form", json.Number("2.0"), "2"},
		{"fractional double", json.Number("1.5"), "1.5"},
		{"exponent double", json.Number("1.0E21"), "1000000000000000000000"},
		{"int64 fixture", int64(7), "7"},
		{"bool", true, "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toString(tt.in)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestToUint64(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want uint64
		ok   bool
	}{
		{"max unsigned_long", json.Number("18446744073709551615"), 18446744073709551615, true},
		{"negative", json.Number("-1"), 0, false},
		{"fraction", json.Number("1.5"), 0, false},
		{"not a number", "7", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toUint64(tt.in)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int64
		ok   bool
	}{
		{"above 2^53", json.Number("9007199254740993"), 9007199254740993, true},
		{"above MaxInt64", json.Number("9223372036854775808"), 0, false},
		{"float64 fixture", float64(42), 42, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toInt64(tt.in)
			require.Equal(t, tt.ok, ok)
			if tt.ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want float64
	}{
		{"exponent double", json.Number("1.0E21"), 1e21},
		{"integral double", json.Number("2.0"), 2},
		{"int64 fixture", int64(3), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := toFloat64(tt.in)
			require.True(t, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseEsqlDateTime_JSONNumberIsEpochMillis(t *testing.T) {
	ts, ok := parseEsqlDateTime(json.Number("1790988431986"))
	require.True(t, ok)
	require.Equal(t, time.UnixMilli(1790988431986), ts)
}

func TestClassifyEsqlColumns_UnsignedLongAndCounterValueColumns(t *testing.T) {
	for _, typ := range []string{"unsigned_long", "counter_long", "counter_integer", "counter_double"} {
		layout := classifyEsqlColumns([]es.EsqlColumn{
			{Name: "t", Type: "date"},
			{Name: "v", Type: typ},
			{Name: "host", Type: "keyword"},
		})
		require.Equal(t, 0, layout.timeColIdx, typ)
		require.Equal(t, 1, layout.valueColIdx, typ)
		require.Equal(t, []int{2}, layout.breakdownColIdxs, typ)
	}
}

func TestProcessEsqlMetricsResponse_JSONNumberValuesAndBreakdowns(t *testing.T) {
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "count(*)", Type: "long"},
			{Name: "job_id", Type: "unsigned_long"},
			{Name: "bucket", Type: "date"},
		},
		Values: [][]any{
			{json.Number("41679"), json.Number("18446744073709551615"), "2026-02-04T00:00:00.000Z"},
			{json.Number("83152"), json.Number("1234567"), "2026-02-04T00:00:00.000Z"},
		},
	}
	target := &Query{
		RefID:    "A",
		RawQuery: "FROM jobs | STATS count(*) BY job_id, bucket = BUCKET(@timestamp, 1 day)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2)

	require.Equal(t, 41679.0, concreteAt(t, res.Frames[0].Fields[1], 0))
	require.Equal(t, data.Labels{"job_id": "18446744073709551615"}, res.Frames[0].Fields[1].Labels)
	require.Equal(t, 83152.0, concreteAt(t, res.Frames[1].Fields[1], 0))
	require.Equal(t, data.Labels{"job_id": "1234567"}, res.Frames[1].Fields[1].Labels)
}

func TestProcessEsqlMetricsResponse_IntegralDoubleBreakdownLabel(t *testing.T) {
	// Elasticsearch writes integral doubles as 2.0. The label is the series
	// identity, so it must keep the form "2" that float64 decoding produced.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "c", Type: "long"},
			{Name: "r", Type: "double"},
			{Name: "b", Type: "date"},
		},
		Values: [][]any{
			{json.Number("1"), json.Number("2.0"), "2026-10-01T00:00:00.000Z"},
			{json.Number("1"), json.Number("3.0"), "2026-10-01T00:00:00.000Z"},
		},
	}
	target := &Query{
		RefID:    "A",
		RawQuery: "FROM issue430 | STATS c = COUNT(*) BY r = ROUND(ratio), b = BUCKET(@timestamp, 1 hour)",
		Metrics:  []*MetricAgg{{Type: countType}},
	}

	res, err := processEsqlMetricsResponse(response, target)
	require.NoError(t, err)
	require.Len(t, res.Frames, 2)
	require.Equal(t, data.Labels{"r": "2"}, res.Frames[0].Fields[1].Labels)
	require.Equal(t, data.Labels{"r": "3"}, res.Frames[1].Fields[1].Labels)
}

func TestProcessEsqlLogsResponse_NumbersBecomeFloat64Fields(t *testing.T) {
	// The logs path shares the _source-shaped document builders with the DSL
	// path, whose numbers are float64. ES|QL numbers must arrive the same way.
	response := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "@timestamp", Type: "date"},
			{Name: "message", Type: "keyword"},
			{Name: "job_id", Type: "unsigned_long"},
			{Name: "lvl", Type: "long"},
		},
		Values: [][]any{
			{"2026-10-01T00:00:00.000Z", "line", json.Number("1234567"), json.Number("3")},
		},
	}

	res, err := processEsqlLogsResponse(response, newLogsDataplaneQuery(t), dataplaneConfiguredFields(), false)
	require.NoError(t, err)
	jobID := fieldByName(t, res.Frames[0], "job_id")
	require.Equal(t, data.FieldTypeNullableFloat64, jobID.Type())
	require.Equal(t, 1234567.0, concreteAt(t, jobID, 0))

	res, err = processEsqlLogsResponse(response, newLogsDataplaneQuery(t), dataplaneConfiguredFields(), true)
	require.NoError(t, err)
	require.Equal(t, "3", concreteAt(t, fieldByName(t, res.Frames[0], "severity"), 0))
}
