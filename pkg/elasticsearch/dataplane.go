package elasticsearch

import (
	"encoding/json"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// dataplaneFeatureToggle gates emission of Grafana dataplane-compliant frames.
// When the toggle is enabled, logs responses are tagged with
// data.FrameTypeLogLines and include canonical timestamp/body/severity/id/labels
// fields as described in https://github.com/grafana/dataplane/blob/main/docs/contract/logs.md.
const dataplaneFeatureToggle = "elasticsearchDataplane"

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

// buildLogLinesCanonicalFields produces the five canonical fields required by
// the Grafana dataplane LogLines contract, in contract order:
// timestamp, body, severity, id, labels.
//
// The docs slice is the same per-hit map the existing processors build; this
// function reads from it without mutation. Unmapped rows fall back to zero
// values (time.Time{}, empty string) so the fields remain non-nullable per
// the contract's "must be non nullable" requirement for timestamp and body.
func buildLogLinesCanonicalFields(docs []map[string]interface{}, configuredFields es.ConfiguredFields) []*data.Field {
	size := len(docs)
	timestamps := make([]time.Time, size)
	bodies := make([]string, size)
	severities := make([]*string, size)
	ids := make([]*string, size)
	labels := make([]json.RawMessage, size)

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
		if bodies[i] == "" {
			if v, ok := doc["_source"].(string); ok {
				bodies[i] = v
			}
		}

		// severity mirrors the "level" field already populated upstream from
		// configuredFields.LogLevelField; left nil when absent per spec (optional).
		if v, ok := doc["level"].(string); ok {
			vv := v
			severities[i] = &vv
		}

		if v, ok := doc["id"].(string); ok {
			vv := v
			ids[i] = &vv
		}

		labels[i] = buildLogLabelsJSON(doc, configuredFields)
	}

	return []*data.Field{
		data.NewField("timestamp", nil, timestamps),
		data.NewField("body", nil, bodies),
		data.NewField("severity", nil, severities),
		data.NewField("id", nil, ids),
		data.NewField("labels", nil, labels),
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

// buildLogLabelsJSON marshals a doc's non-canonical fields into a single JSON
// object, suitable for use as the LogLines `labels` field.
//
// Excluded keys: the configured time, message, and level source fields (those
// are promoted to canonical fields); the internally computed "id" and "level"
// mirrors; and "_source" (the whole-document JSON blob, which would duplicate
// every other field).
func buildLogLabelsJSON(doc map[string]interface{}, configuredFields es.ConfiguredFields) json.RawMessage {
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
	for k, v := range doc {
		if _, skip := excluded[k]; skip {
			continue
		}
		filtered[k] = v
	}
	if len(filtered) == 0 {
		return json.RawMessage("{}")
	}
	bytes, err := json.Marshal(filtered)
	if err != nil {
		return json.RawMessage("{}")
	}
	return json.RawMessage(bytes)
}
