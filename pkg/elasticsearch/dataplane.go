package elasticsearch

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// Dataplane-compliant logs frames are gated behind the featureflags.LogsDataplane
// GOFF flag (see pkg/elasticsearch/featureflags). When enabled, logs responses
// are tagged with data.FrameTypeLogLines and carry the canonical
// timestamp/body/severity/id/labels/labelTypes fields described in
// https://github.com/grafana/dataplane/blob/main/docs/contract/logs.md.
//
// The flag is scoped to logs specifically to leave room for a separate
// metrics-dataplane flag later (mirroring lokiLogsDataplane / lokiMetricDataplane).

// labelTypeField marks a label as a regular log field (from _source or the
// hit envelope).
const labelTypeField = "Field"

// labelTypeMetadata marks a label as metadata: a value the fields API returned
// that the document _source does not carry, such as a runtime field.
const labelTypeMetadata = "Metadata"

// labelTypeArrayField marks a label whose value is a JSON array.
const labelTypeArrayField = "ArrayField"

// setLogLinesFrameMeta tags a frame with the LogLines dataplane type.
// Callers must have already prepended the canonical fields so that the
// first time field is named "timestamp" and the first string field is
// named "body" as required by the contract.
func setLogLinesFrameMeta(frame *data.Frame) {
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	frame.Meta.Type = data.FrameTypeLogLines
	frame.Meta.TypeVersion = data.FrameTypeVersion{0, 0}
}

// buildLogLinesCanonicalFields produces the six canonical fields required by
// the Grafana dataplane LogLines contract, in contract order:
// timestamp, body, severity, id, labels, labelTypes.
//
// rowIDs[i] is the log-line id for docs[i], derived by the caller from the
// hit envelope (or the ES|QL row) rather than from any `id` attribute the
// document carries, so two documents can never share an id and a document's
// own `id` stays a label.
//
// metadataKeys[i] holds the doc keys for hit i that hit["fields"] returned
// and _source does not carry. These are tagged as "Metadata" in labelTypes;
// everything else is "Field" or "ArrayField"
// depending on its runtime type. Pass nil when no such distinction exists
// (e.g. ES|QL responses, where all keys are Field-equivalent columns).
//
// timestamp and body are non-nullable per the spec; rows with no parsable
// time stay at the zero time.Time (whether to omit them with a notice instead
// is tracked in #317) and rows with no body stay at "".
func buildLogLinesCanonicalFields(docs []map[string]interface{}, rowIDs []string, configuredFields es.ConfiguredFields, metadataKeys []map[string]struct{}) []*data.Field {
	size := len(docs)
	timestamps := make([]time.Time, size)
	bodies := make([]string, size)
	severities := make([]*string, size)
	ids := make([]*string, size)
	labels := make([]json.RawMessage, size)
	labelTypes := make([]json.RawMessage, size)

	for i, doc := range docs {
		if configuredFields.TimeField != "" {
			if t, ok := parseDocTimeValue(doc[configuredFields.TimeField]); ok {
				timestamps[i] = t
			}
		}

		if configuredFields.LogMessageField != "" {
			if v, ok := doc[configuredFields.LogMessageField].(string); ok {
				bodies[i] = v
			}
		}

		// severity reads the configured level field directly, like timestamp
		// and body, so a value that only arrives via hit["fields"] is seen and
		// a document's own "level" attribute cannot shadow the configured one.
		// Without a configured field the document's "level" is used, which is
		// what legacy frames exposed. The value passes through unchanged:
		// Grafana matches it case-insensitively against its level aliases and
		// the numeric levels 0-7, so only non-string scalars need converting to
		// reach this string field. Left nil when absent per spec.
		levelKey := "level"
		if configuredFields.LogLevelField != "" {
			levelKey = configuredFields.LogLevelField
		}
		if s, ok := scalarString(doc[levelKey]); ok {
			severities[i] = &s
		}

		if i < len(rowIDs) && rowIDs[i] != "" {
			id := rowIDs[i]
			ids[i] = &id
		}

		var meta map[string]struct{}
		if i < len(metadataKeys) {
			meta = metadataKeys[i]
		}
		labels[i], labelTypes[i] = buildLogLabelsAndTypes(doc, configuredFields, meta)
	}

	return []*data.Field{
		data.NewField("timestamp", nil, timestamps),
		data.NewField("body", nil, bodies),
		data.NewField("severity", nil, severities),
		data.NewField("id", nil, ids),
		data.NewField("labels", nil, labels),
		data.NewField("labelTypes", nil, labelTypes),
	}
}

// prependLogLinesCanonicalFields returns the canonical fields followed by the
// legacy fields, minus any legacy field that shares a canonical name. The
// contract binds each role to the first field with that name, so a same-named
// legacy field is never read and only shows up as a duplicate column or extra
// field. Its value survives in the canonical field it collided with, or in
// labels when it came from the document.
func prependLogLinesCanonicalFields(canonical, legacy []*data.Field) []*data.Field {
	names := make(map[string]struct{}, len(canonical))
	for _, f := range canonical {
		names[f.Name] = struct{}{}
	}
	fields := make([]*data.Field, 0, len(canonical)+len(legacy))
	fields = append(fields, canonical...)
	for _, f := range legacy {
		if _, taken := names[f.Name]; taken {
			continue
		}
		fields = append(fields, f)
	}
	return fields
}

// parseDocTimeValue parses a time value out of a doc field: an RFC3339Nano
// string, the single-element array Elasticsearch's "fields" response uses, or
// the epoch-millis number the default date mapping accepts and raw DSL
// queries read back from _source unchanged.
func parseDocTimeValue(v interface{}) (time.Time, bool) {
	if arr, ok := v.([]interface{}); ok && len(arr) == 1 {
		v = arr[0]
	}
	switch value := v.(type) {
	case string:
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, false
		}
		return t, true
	case float64:
		return time.UnixMilli(int64(value)).UTC(), true
	}
	return time.Time{}, false
}

// scalarString returns the string form of a scalar document value. Level
// fields are commonly numeric (syslog severity codes, OTel SeverityNumber),
// and JSON numbers decode as float64, so they are formatted without a
// fractional part.
func scalarString(v interface{}) (string, bool) {
	switch value := v.(type) {
	case string:
		return value, true
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	}
	return "", false
}

// buildLogLabelsAndTypes marshals a doc's non-canonical fields into two JSON
// objects: the `labels` payload (a Record<string,any> of key→value) and the
// `labelTypes` payload (a Record<string,string> of key→category).
//
// Excluded keys: the configured time and message fields (promoted to the
// canonical timestamp and body, which consumers read from there); the
// internal "level" mirror when it copies a differently named configured
// field; "_source" (the whole-document JSON blob, which would duplicate
// every other field); and the hit envelope keys "_type", "sort" and
// "highlight", which describe the search response rather than the document.
// "_type" is absent on Elasticsearch 8 and later, so it would surface as null.
//
// The configured level field stays a label even though it also feeds
// severity: Grafana's Log Details for LogLines frames lists labels only, so
// this is what keeps the level visible and click-filterable there. A
// document's own "id" attribute stays too; the canonical id comes from the
// hit envelope, not from it.
//
// metadataKeys names the keys that hit["fields"] returned and _source does not
// carry. Those become "Metadata"; values whose runtime type is an array become
// "ArrayField"; everything else is "Field".
func buildLogLabelsAndTypes(doc map[string]interface{}, configuredFields es.ConfiguredFields, metadataKeys map[string]struct{}) (json.RawMessage, json.RawMessage) {
	excluded := map[string]struct{}{
		"_source":   {},
		"_type":     {},
		"sort":      {},
		"highlight": {},
	}
	if configuredFields.TimeField != "" {
		excluded[configuredFields.TimeField] = struct{}{}
	}
	if configuredFields.LogMessageField != "" {
		excluded[configuredFields.LogMessageField] = struct{}{}
	}
	if configuredFields.LogLevelField != "" && configuredFields.LogLevelField != "level" {
		excluded["level"] = struct{}{}
	}

	filtered := make(map[string]interface{}, len(doc))
	types := make(map[string]string, len(doc))
	for k, v := range doc {
		if _, skip := excluded[k]; skip {
			continue
		}
		filtered[k] = v
		types[k] = classifyLabelType(k, v, metadataKeys)
	}

	if len(filtered) == 0 {
		return json.RawMessage("{}"), json.RawMessage("{}")
	}

	labelsBytes, err := json.Marshal(filtered)
	if err != nil {
		return json.RawMessage("{}"), json.RawMessage("{}")
	}
	typesBytes, err := json.Marshal(types)
	if err != nil {
		return json.RawMessage(labelsBytes), json.RawMessage("{}")
	}
	return json.RawMessage(labelsBytes), json.RawMessage(typesBytes)
}

// classifyLabelType returns the dataplane labelTypes category for a doc field.
// Array values take priority over Metadata so consumers see the structural hint
// regardless of where the value originated.
func classifyLabelType(key string, value interface{}, metadataKeys map[string]struct{}) string {
	if _, ok := value.([]interface{}); ok {
		return labelTypeArrayField
	}
	if _, ok := metadataKeys[key]; ok {
		return labelTypeMetadata
	}
	return labelTypeField
}
