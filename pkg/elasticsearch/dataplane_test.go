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

func countFieldsNamed(frame *data.Frame, name string) int {
	n := 0
	for _, f := range frame.Fields {
		if f.Name == name {
			n++
		}
	}
	return n
}

var logLinesCanonicalNames = []string{"timestamp", "body", "severity", "id", "labels", "labelTypes"}

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
			"sort":      []interface{}{float64(1704164645123), "doc-1"},
			"highlight": map[string]interface{}{"message": []interface{}{"<em>hello</em> world"}},
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
		for _, name := range logLinesCanonicalNames {
			require.Equalf(t, 1, countFieldsNamed(frame, name), "canonical field %q must appear exactly once", name)
		}

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
		require.Equal(t, "info", labels["lvl"], "the configured level field stays a label so Log Details can show and filter on it")
		require.NotContains(t, labels, "level", "the internal level mirror is not a document attribute")
		require.NotContains(t, labels, "id")
		require.NotContains(t, labels, "_source")
		for _, k := range []string{"_type", "sort", "highlight"} {
			require.NotContainsf(t, labels, k, "hit envelope key %q is not a document attribute", k)
		}

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
		for _, name := range logLinesCanonicalNames {
			require.Equalf(t, 1, countFieldsNamed(frame, name), "canonical field %q must appear exactly once", name)
		}

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
		require.Equal(t, "warn", labels["lvl"], "the configured level field stays a label")

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

func TestLogsResponseProcessor_DataplaneCanonicalNameWinsOverLegacy(t *testing.T) {
	// A source whose time and message fields are literally named "timestamp"
	// and "body" would otherwise produce two fields of each name.
	configuredFields := es.ConfiguredFields{
		TimeField:       "timestamp",
		LogMessageField: "body",
	}
	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"timestamp": "2024-01-02T03:04:05.123Z",
				"body":      "hello world",
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

	for _, name := range logLinesCanonicalNames {
		require.Equalf(t, 1, countFieldsNamed(frame, name), "canonical field %q must appear exactly once", name)
	}
	expected, _ := time.Parse(time.RFC3339Nano, "2024-01-02T03:04:05.123Z")
	require.Equal(t, expected, fieldByName(t, frame, "timestamp").At(0).(time.Time), "the non-nullable canonical timestamp must be the one that survives")
	require.Equal(t, "hello world", fieldByName(t, frame, "body").At(0).(string))
}

func TestBuildLogLabelsAndTypes_EmptyWhenNothingRemains(t *testing.T) {
	configuredFields := dataplaneConfiguredFields()
	doc := map[string]interface{}{
		"@timestamp": "2024-01-02T00:00:00Z",
		"message":    "m",
		"level":      "info",
		"_source":    "{}",
		"_type":      "_doc",
		"sort":       []interface{}{float64(1)},
		"highlight":  map[string]interface{}{},
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

func TestLogsResponseProcessor_DataplaneIDComesFromHitEnvelope(t *testing.T) {
	// A document's own `id` attribute must not become the row id: a numeric
	// value would be dropped and a repeated string would make rows share an
	// id. It stays a label instead.
	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"message":    "numeric id",
				"id":         float64(42),
			},
		},
		{
			"_id":    "doc-2",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:06.456Z",
				"message":    "string id",
				"id":         "user-7",
			},
		},
		{
			"_id":    "doc-3",
			"_index": "logs-000001",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:07.789Z",
				"message":    "same string id",
				"id":         "user-7",
			},
		},
	}
	searchResponse := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits:  hits,
			Total: &es.SearchResponseHitsTotal{Value: len(hits), Relation: "eq"},
		},
	}

	processor := newLogsResponseProcessor(log.New())
	queryRes := backend.DataResponse{}
	err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), dataplaneConfiguredFields(), true, &queryRes)
	require.NoError(t, err)
	frame := queryRes.Frames[0]

	require.Equal(t, 1, countFieldsNamed(frame, "id"))
	idField := fieldByName(t, frame, "id")
	labelsField := fieldByName(t, frame, "labels")
	labelTypesField := fieldByName(t, frame, "labelTypes")
	wantIDs := []string{"logs-000001#doc-1", "logs-000001#doc-2", "logs-000001#doc-3"}
	wantLabelIDs := []interface{}{float64(42), "user-7", "user-7"}
	for i := range hits {
		id := idField.At(i).(*string)
		require.NotNil(t, id)
		require.Equal(t, wantIDs[i], *id)

		var labels map[string]interface{}
		require.NoError(t, json.Unmarshal(labelsField.At(i).(json.RawMessage), &labels))
		require.Equal(t, wantLabelIDs[i], labels["id"], "the document's own id stays a label")
		var types map[string]string
		require.NoError(t, json.Unmarshal(labelTypesField.At(i).(json.RawMessage), &types))
		require.Equal(t, labelTypeField, types["id"])
	}
}

func TestEsqlLogsResponseProcessor_DataplaneIDFromMetadataColumns(t *testing.T) {
	esqlResp := &es.EsqlResponse{
		Columns: []es.EsqlColumn{
			{Name: "@timestamp", Type: "date"},
			{Name: "message", Type: "keyword"},
			{Name: "_index", Type: "keyword"},
			{Name: "_id", Type: "keyword"},
			{Name: "id", Type: "long"},
		},
		Values: [][]any{
			{"2024-05-01T12:00:00.000Z", "with metadata", "logs-2024", "abc", float64(1)},
			{"2024-05-01T12:00:01.000Z", "without metadata", nil, nil, nil},
		},
	}

	resp, err := processEsqlLogsResponse(esqlResp, newLogsDataplaneQuery(t), dataplaneConfiguredFields(), true)
	require.NoError(t, err)
	frame := resp.Frames[0]

	require.Equal(t, 1, countFieldsNamed(frame, "id"))
	idField := fieldByName(t, frame, "id")
	id0 := idField.At(0).(*string)
	require.NotNil(t, id0)
	require.Equal(t, "logs-2024#abc", *id0, "METADATA _index, _id give the same identity as the search path")
	id1 := idField.At(1).(*string)
	require.NotNil(t, id1)
	require.Equal(t, "esql-row-1", *id1, "without METADATA _id the row position is the only handle")

	var labels map[string]interface{}
	require.NoError(t, json.Unmarshal(fieldByName(t, frame, "labels").At(0).(json.RawMessage), &labels))
	require.Equal(t, float64(1), labels["id"], "a column named id stays a label")
	require.Equal(t, "abc", labels["_id"])
}

func TestBuildLogLabelsAndTypes_KeepsLevelSourceAndDocumentID(t *testing.T) {
	// The configured level field is what Log Details shows and filters on,
	// and a document's own `id` is an ordinary attribute now that the
	// canonical id comes from the hit envelope.
	labels, types := buildLogLabelsAndTypes(map[string]interface{}{
		"lvl":   "info",
		"level": "info",
		"id":    float64(42),
	}, dataplaneConfiguredFields(), nil)
	require.JSONEq(t, `{"lvl":"info","id":42}`, string(labels))
	require.JSONEq(t, `{"lvl":"Field","id":"Field"}`, string(types))
}

func TestBuildLogLabelsAndTypes_LevelStaysWhenItIsTheConfiguredField(t *testing.T) {
	labels, _ := buildLogLabelsAndTypes(map[string]interface{}{"level": "info"}, es.ConfiguredFields{LogLevelField: "level"}, nil)
	require.JSONEq(t, `{"level":"info"}`, string(labels))
}

func TestLogsResponseProcessor_DataplaneSeverityReadsConfiguredField(t *testing.T) {
	configuredFields := es.ConfiguredFields{
		TimeField:       "@timestamp",
		LogMessageField: "message",
		LogLevelField:   "sev",
	}
	hits := []map[string]interface{}{
		{
			// Numeric levels (syslog codes, OTel SeverityNumber) decode as float64.
			"_id":    "doc-1",
			"_index": "idx",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"message":    "numeric level",
				"sev":        float64(3),
			},
		},
		{
			// _source disabled, or a runtime field configured as the level
			// field: the value arrives only via hit.fields.
			"_id":    "doc-2",
			"_index": "idx",
			"fields": map[string]interface{}{
				"@timestamp": []interface{}{"2024-01-02T03:04:06.456Z"},
				"message":    []interface{}{"fields only"},
				"sev":        []interface{}{"error"},
			},
		},
		{
			// A document's own `level` attribute must not shadow the configured field.
			"_id":    "doc-3",
			"_index": "idx",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:07.789Z",
				"message":    "both keys",
				"sev":        "warn",
				"level":      "from-level",
			},
		},
	}
	searchResponse := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits:  hits,
			Total: &es.SearchResponseHitsTotal{Value: len(hits), Relation: "eq"},
		},
	}

	processor := newLogsResponseProcessor(log.New())
	queryRes := backend.DataResponse{}
	err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), configuredFields, true, &queryRes)
	require.NoError(t, err)
	frame := queryRes.Frames[0]

	severityField := fieldByName(t, frame, "severity")
	for i, want := range []string{"3", "error", "warn"} {
		got := severityField.At(i).(*string)
		require.NotNil(t, got, "row %d", i)
		require.Equal(t, want, *got, "row %d", i)
	}

	var labels map[string]interface{}
	require.NoError(t, json.Unmarshal(fieldByName(t, frame, "labels").At(0).(json.RawMessage), &labels))
	require.Equal(t, float64(3), labels["sev"], "the configured level field stays a label")
	require.NotContains(t, labels, "level", "the internal level mirror is not a document attribute")
}

func TestLogsResponseProcessor_DataplaneSeverityFromKeywordSubfield(t *testing.T) {
	// A common configuration points LogLevelField at a keyword multi-field,
	// which never appears in _source and only arrives via hit.fields.
	configuredFields := es.ConfiguredFields{
		TimeField:       "@timestamp",
		LogMessageField: "message",
		LogLevelField:   "lvl.keyword",
	}
	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_index": "idx",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"message":    "keyword level",
				"lvl":        "error",
			},
			"fields": map[string]interface{}{
				"lvl.keyword": []interface{}{"error"},
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

	got := fieldByName(t, frame, "severity").At(0).(*string)
	require.NotNil(t, got)
	require.Equal(t, "error", *got)
	var labels map[string]interface{}
	require.NoError(t, json.Unmarshal(fieldByName(t, frame, "labels").At(0).(json.RawMessage), &labels))
	require.Equal(t, "error", labels["lvl.keyword"])
}

func TestLogsResponseProcessor_DataplaneUnconfiguredLevelAttributeStaysALabel(t *testing.T) {
	// With no LogLevelField, a document's own `level` is what legacy frames
	// exposed and what Grafana read as the level. It is promoted to severity
	// and kept as a label so Log Details still shows it.
	configuredFields := es.ConfiguredFields{
		TimeField:       "@timestamp",
		LogMessageField: "message",
	}
	hits := []map[string]interface{}{
		{
			"_id":    "doc-1",
			"_index": "idx",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:05.123Z",
				"message":    "upper-case level",
				"level":      "INFO",
			},
		},
		{
			"_id":    "doc-2",
			"_index": "idx",
			"_source": map[string]interface{}{
				"@timestamp": "2024-01-02T03:04:06.456Z",
				"message":    "numeric level",
				"level":      float64(3),
			},
		},
	}
	searchResponse := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits:  hits,
			Total: &es.SearchResponseHitsTotal{Value: len(hits), Relation: "eq"},
		},
	}

	processor := newLogsResponseProcessor(log.New())
	queryRes := backend.DataResponse{}
	err := processor.processLogsResponse(searchResponse, newLogsDataplaneQuery(t), configuredFields, true, &queryRes)
	require.NoError(t, err)
	frame := queryRes.Frames[0]

	severityField := fieldByName(t, frame, "severity")
	labelsField := fieldByName(t, frame, "labels")
	wantSeverity := []string{"INFO", "3"}
	wantLabel := []interface{}{"INFO", float64(3)}
	for i := range hits {
		got := severityField.At(i).(*string)
		require.NotNil(t, got, "row %d", i)
		require.Equal(t, wantSeverity[i], *got, "row %d", i)

		var labels map[string]interface{}
		require.NoError(t, json.Unmarshal(labelsField.At(i).(json.RawMessage), &labels))
		require.Equal(t, wantLabel[i], labels["level"], "row %d", i)
	}
}

func TestParseDocTimeValue(t *testing.T) {
	want := time.Date(2024, 1, 2, 3, 4, 5, 123000000, time.UTC)
	tests := []struct {
		name  string
		value interface{}
		want  time.Time
		ok    bool
	}{
		{name: "RFC3339Nano string", value: "2024-01-02T03:04:05.123Z", want: want, ok: true},
		{name: "single-element fields array", value: []interface{}{"2024-01-02T03:04:05.123Z"}, want: want, ok: true},
		{name: "epoch millis from _source", value: float64(1704164645123), want: want, ok: true},
		{name: "custom date format", value: "09/02/2023"},
		{name: "multi-valued array", value: []interface{}{"2024-01-02T03:04:05.123Z", "2024-01-02T03:04:06.456Z"}},
		{name: "absent", value: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDocTimeValue(tt.value)
			require.Equal(t, tt.ok, ok)
			require.True(t, tt.want.Equal(got), "got %s", got)
		})
	}
}

func TestBuildLogLinesCanonicalFields_UnparsableTimeStaysZero(t *testing.T) {
	// Documented behaviour, pinned so any change is deliberate: timestamp is
	// non-nullable, and a row whose time is absent or in a format the parser
	// does not know keeps the zero time.Time. Whether such rows should be
	// omitted with a notice instead is tracked in #317.
	docs := []map[string]interface{}{
		{"@timestamp": "09/02/2023", "message": "custom format"},
		{"message": "no time at all"},
	}
	fields := buildLogLinesCanonicalFields(docs, []string{"a", "b"}, dataplaneConfiguredFields(), nil)
	require.Equal(t, "timestamp", fields[0].Name)
	require.True(t, fields[0].At(0).(time.Time).IsZero())
	require.True(t, fields[0].At(1).(time.Time).IsZero())
}
