package elasticsearch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

func newLogsDataplaneQuery(t *testing.T) *Query {
	t.Helper()
	settings, err := simplejson.NewJson([]byte(`{"limit":"500"}`))
	require.NoError(t, err)
	return &Query{
		RefID: "A",
		Metrics: []*MetricAgg{
			{Type: "logs", ID: "1", Settings: settings},
		},
	}
}

func dataplaneConfiguredFields() es.ConfiguredFields {
	return es.ConfiguredFields{
		TimeField:       "@timestamp",
		LogMessageField: "message",
		LogLevelField:   "lvl",
	}
}

func fieldByName(t *testing.T, frame *data.Frame, name string) *data.Field {
	t.Helper()
	for _, f := range frame.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not found on frame", name)
	return nil
}

func TestLogsResponseProcessor_Dataplane(t *testing.T) {
	configuredFields := dataplaneConfiguredFields()

	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_type":  "_doc",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"message":    "hello world",
				"lvl":        "info",
				"host":       "host-a",
				"tags":       []interface{}{"alpha", "beta"},
			},
			"fields": map[string]interface{}{
				"region": []interface{}{"us-east-1"},
			},
		},
		{
			"_id":    "doc-2",
			"_type":  "_doc",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:06.456Z",
				"message":    "second line",
				"lvl":        "error",
				"host":       "host-b",
			},
		},
	}
	total := 2
	searchResponse := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits:  hits,
			Total: &es.SearchResponseHitsTotal{Value: total, Relation: "eq"},
		},
	}

	t.Run("flag off: frame lacks dataplane meta and canonical fields", func(t *testing.T) {
		processor := newLogsResponseProcessor(log.New())
		queryRes := backend.DataResponse{}
		err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), configuredFields, false, &queryRes)
		require.NoError(t, err)
		require.Len(t, queryRes.Frames, 1)
		frame := queryRes.Frames[0]

		require.NotNil(t, frame.Meta)
		require.Empty(t, string(frame.Meta.Type))
		require.Equal(t, data.VisTypeLogs, string(frame.Meta.PreferredVisualization))

		for _, name := range []string{"timestamp", "body", "severity", "labels", "labelTypes"} {
			for _, f := range frame.Fields {
				require.NotEqualf(t, name, f.Name, "canonical field %q should not exist when flag is off", name)
			}
		}
	})

	t.Run("flag on: frame carries LogLines meta and canonical fields", func(t *testing.T) {
		processor := newLogsResponseProcessor(log.New())
		queryRes := backend.DataResponse{}
		err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), configuredFields, true, &queryRes)
		require.NoError(t, err)
		require.Len(t, queryRes.Frames, 1)
		frame := queryRes.Frames[0]

		require.NotNil(t, frame.Meta)
		require.Equal(t, data.FrameTypeLogLines, frame.Meta.Type)
		require.Equal(t, data.FrameTypeVersion{0, 0}, frame.Meta.TypeVersion)
		require.Equal(t, data.VisTypeLogs, string(frame.Meta.PreferredVisualization))

		require.Equal(t, "timestamp", frame.Fields[0].Name)
		require.Equal(t, data.FieldTypeTime, frame.Fields[0].Type())
		require.Equal(t, "body", frame.Fields[1].Name)
		require.Equal(t, data.FieldTypeString, frame.Fields[1].Type())
		require.Equal(t, "severity", frame.Fields[2].Name)
		require.Equal(t, "id", frame.Fields[3].Name)
		require.Equal(t, "labels", frame.Fields[4].Name)
		require.Equal(t, "labelTypes", frame.Fields[5].Name)

		bodyField := fieldByName(t, frame, "body")
		require.Equal(t, "hello world", bodyField.At(0).(string))
		require.Equal(t, "second line", bodyField.At(1).(string))

		timestampField := fieldByName(t, frame, "timestamp")
		expected0, _ := time.Parse(time.RFC3339Nano, "2024-01-02T03:04:05.123Z")
		require.Equal(t, expected0, timestampField.At(0).(time.Time))

		severityField := fieldByName(t, frame, "severity")
		sev0 := severityField.At(0).(*string)
		require.NotNil(t, sev0)
		require.Equal(t, "info", *sev0)

		idField := fieldByName(t, frame, "id")
		id0 := idField.At(0).(*string)
		require.NotNil(t, id0)
		require.Equal(t, "logs-000001#doc-1", *id0)

		labelsField := fieldByName(t, frame, "labels")
		raw := labelsField.At(0).(json.RawMessage)
		var labels map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &labels))
		require.Equal(t, "host-a", labels["host"])
		require.Equal(t, "logs-000001", labels["_index"])
		require.Equal(t, "us-east-1", labels["region"], "doc-value field should be unwrapped from its array")
		require.NotContains(t, labels, "@timestamp")
		require.NotContains(t, labels, "message")
		require.NotContains(t, labels, "lvl")
		require.NotContains(t, labels, "level")
		require.NotContains(t, labels, "id")
		require.NotContains(t, labels, "_source")

		labelTypesField := fieldByName(t, frame, "labelTypes")
		rawTypes := labelTypesField.At(0).(json.RawMessage)
		var types map[string]string
		require.NoError(t, json.Unmarshal(rawTypes, &types))
		require.Equal(t, labelTypeField, types["host"], "host comes from _source → Field")
		require.Equal(t, labelTypeMetadata, types["region"], "region came from hit.fields → Metadata")
		require.Equal(t, labelTypeArrayField, types["tags"], "tags is a JSON array → ArrayField")
	})
}

func TestEsqlLogsResponseProcessor_Dataplane(t *testing.T) {
	configuredFields := dataplaneConfiguredFields()

	esqlResp := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "@timestamp", Type: "date"},
			{Name: "message", Type: "keyword"},
			{Name: "lvl", Type: "keyword"},
			{Name: "host", Type: "keyword"},
		},
		Values: [][]any{
			{"2024-05-01T12:00:00.000Z", "esql line one", "warn", "host-a"},
			{"2024-05-01T12:00:01.000Z", "esql line two", "error", "host-b"},
		},
	}

	t.Run("flag off: no dataplane meta or canonical fields", func(t *testing.T) {
		resp, err := processEsqlLogsResponse(esqlResp, newLogsDataplaneQuery(t), configuredFields, false)
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		frame := resp.Frames[0]
		require.Empty(t, string(frame.Meta.Type))
		for _, f := range frame.Fields {
			require.NotEqual(t, "timestamp", f.Name)
			require.NotEqual(t, "body", f.Name)
			require.NotEqual(t, "labelTypes", f.Name)
		}
	})

	t.Run("flag on: LogLines meta and canonical fields are prepended", func(t *testing.T) {
		resp, err := processEsqlLogsResponse(esqlResp, newLogsDataplaneQuery(t), configuredFields, true)
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		frame := resp.Frames[0]

		require.Equal(t, data.FrameTypeLogLines, frame.Meta.Type)
		require.Equal(t, data.FrameTypeVersion{0, 0}, frame.Meta.TypeVersion)
		require.Equal(t, "timestamp", frame.Fields[0].Name)
		require.Equal(t, "body", frame.Fields[1].Name)
		require.Equal(t, "labelTypes", frame.Fields[5].Name)

		bodyField := fieldByName(t, frame, "body")
		require.Equal(t, "esql line one", bodyField.At(0).(string))

		severityField := fieldByName(t, frame, "severity")
		sev0 := severityField.At(0).(*string)
		require.NotNil(t, sev0)
		require.Equal(t, "warn", *sev0)

		labelsField := fieldByName(t, frame, "labels")
		raw := labelsField.At(0).(json.RawMessage)
		var labels map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &labels))
		require.Equal(t, "host-a", labels["host"])
		require.NotContains(t, labels, "@timestamp")
		require.NotContains(t, labels, "message")
		require.NotContains(t, labels, "lvl")

		labelTypesField := fieldByName(t, frame, "labelTypes")
		rawTypes := labelTypesField.At(0).(json.RawMessage)
		var types map[string]string
		require.NoError(t, json.Unmarshal(rawTypes, &types))
		require.Equal(t, labelTypeField, types["host"], "ES|QL columns are all Field-category")
	})
}

func TestLogsResponseProcessor_DataplaneBodyDoesNotFallBackToSource(t *testing.T) {
	// When LogMessageField is unset, body should stay empty rather than
	// falling back to the whole flattened _source JSON blob. The spec
	// requires body to be a non-nullable string; "" is compliant and
	// honest, whereas a JSON-encoded doc isn't a meaningful "message".
	configuredFields := es.ConfiguredFields{
		TimeField:       "@timestamp",
		LogMessageField: "",
		LogLevelField:   "lvl",
	}

	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_type":  "_doc",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"lvl":        "info",
				"host":       "host-a",
			},
		},
	}
	searchResponse := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits:  hits,
			Total: &es.SearchResponseHitsTotal{Value: 1, Relation: "eq"},
		},
	}

	processor := newLogsResponseProcessor(log.New())
	queryRes := backend.DataResponse{}
	err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), configuredFields, true, &queryRes)
	require.NoError(t, err)
	frame := queryRes.Frames[0]

	bodyField := fieldByName(t, frame, "body")
	require.Equal(t, "", bodyField.At(0).(string), "body must be empty (not _source JSON) when LogMessageField is unset")
}

func TestBuildLogLabelsAndTypes_EmptyWhenNothingRemains(t *testing.T) {
	configuredFields := dataplaneConfiguredFields()
	doc := map[string]interface{}{
		"@timestamp": "2024-01-02T00:00:00Z",
		"message":    "m",
		"lvl":        "info",
		"level":      "info",
		"id":         "x",
		"_source":    "{}",
	}
	labels, types := buildLogLabelsAndTypes(doc, configuredFields, nil)
	require.Equal(t, "{}", string(labels))
	require.Equal(t, "{}", string(types))
}

func TestClassifyLabelType(t *testing.T) {
	t.Run("array value beats metadata flag", func(t *testing.T) {
		got := classifyLabelType("region", []interface{}{"us-east-1"}, map[string]struct{}{"region": {}})
		require.Equal(t, labelTypeArrayField, got)
	})
	t.Run("metadata flag wins over scalar fallback", func(t *testing.T) {
		got := classifyLabelType("region", "us-east-1", map[string]struct{}{"region": {}})
		require.Equal(t, labelTypeMetadata, got)
	})
	t.Run("plain scalar is Field", func(t *testing.T) {
		got := classifyLabelType("host", "host-a", nil)
		require.Equal(t, labelTypeField, got)
	})
}
