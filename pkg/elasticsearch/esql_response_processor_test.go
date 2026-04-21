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
