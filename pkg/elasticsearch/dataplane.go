package elasticsearch

import (
	"encoding/json"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// dataplaneFeatureToggle gates emission of Grafana dataplane-compliant logs
// frames. When enabled, logs responses are tagged with data.FrameTypeLogLines
// and carry the canonical timestamp/body/severity/id/labels/labelTypes fields
// described in https://github.com/grafana/dataplane/blob/main/docs/contract/logs.md.
//
// The toggle is scoped to logs specifically to leave room for a separate
// metrics-dataplane toggle later (mirroring lokiLogsDataplane / lokiMetricDataplane).
const dataplaneFeatureToggle = "elasticsearchLogsDataplane"

// labelTypeField marks a label as a regular log field (from _source).
const labelTypeField = "Field"

// labelTypeMetadata marks a label as metadata, e.g. a doc-value returned via
// the `fields` parameter rather than the document _source.
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
// metadataKeys[i] holds the doc keys for hit i that originated from
// hit["fields"] (doc-value returns) rather than _source. These are tagged
// as "Metadata" in labelTypes; everything else is "Field" or "ArrayField"
// depending on its runtime type. Pass nil when no such distinction exists
// (e.g. ES|QL responses, where all keys are Field-equivalent columns).
//
// timestamp and body are non-nullable per the spec; rows with no parsable
// time stay at the zero time.Time and rows with no body stay at "".
func buildLogLinesCanonicalFields(docs []map[string]interface{}, configuredFields es.ConfiguredFields, metadataKeys []map[string]struct{}) []*data.Field {
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

		// severity mirrors the "level" field already populated upstream from
		// configuredFields.LogLevelField. Pass-through: values are expected to
		// already match Grafana's log-level enum (critical/error/warning/info/
		// debug/trace) — this contract is enforced by data-source config, not
		// here. Left nil when absent per spec.
		if v, ok := doc["level"].(string); ok {
			vv := v
			severities[i] = &vv
		}

		if v, ok := doc["id"].(string); ok {
			vv := v
			ids[i] = &vv
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

// parseDocTimeValue parses a time value out of a doc field, handling both the
// plain RFC3339Nano string case and the single-element array case that
// Elasticsearch's "fields" response uses.
func parseDocTimeValue(v interface{}) (time.Time, bool) {
	s, ok := v.(string)
	if !ok {
		if arr, arrOk := v.([]interface{}); arrOk && len(arr) == 1 {
			s, ok = arr[0].(string)
		}
	}
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// buildLogLabelsAndTypes marshals a doc's non-canonical fields into two JSON
// objects: the `labels` payload (a Record<string,any> of key→value) and the
// `labelTypes` payload (a Record<string,string> of key→category).
//
// Excluded keys: the configured time, message, and level source fields (those
// are promoted to canonical fields); the internally computed "id" and "level"
// mirrors; and "_source" (the whole-document JSON blob, which would duplicate
// every other field).
//
// metadataKeys names the keys that originated from hit["fields"] (doc-value
// returns) rather than _source. Those become "Metadata"; values whose runtime
// type is an array become "ArrayField"; everything else is "Field".
func buildLogLabelsAndTypes(doc map[string]interface{}, configuredFields es.ConfiguredFields, metadataKeys map[string]struct{}) (json.RawMessage, json.RawMessage) {
	excluded := map[string]struct{}{
		"id":      {},
		"level":   {},
		"_source": {},
	}
	if configuredFields.TimeField != "" {
		excluded[configuredFields.TimeField] = struct{}{}
	}
	if configuredFields.LogMessageField != "" {
		excluded[configuredFields.LogMessageField] = struct{}{}
	}
	if configuredFields.LogLevelField != "" {
		excluded[configuredFields.LogLevelField] = struct{}{}
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
