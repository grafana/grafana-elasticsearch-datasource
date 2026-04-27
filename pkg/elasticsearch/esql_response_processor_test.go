package elasticsearch

import (
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
