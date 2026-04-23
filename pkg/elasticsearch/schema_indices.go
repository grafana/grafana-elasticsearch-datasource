package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	schemas "github.com/grafana/schemads"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

const (
	schemaIndicesCacheTTL = 2 * time.Minute
	maxFieldColumns       = 2000
)

type catIndexRow struct {
	Index string `json:"index"`
}

// listAllIndexNames returns sorted index names (filtered). Truncates to s.MaxIndices if the cluster exceeds that.
//
// Tries _resolve/index/* first because it is lighter weight, has friendlier
// permission requirements (view_index_metadata vs the monitor cluster privilege
// required by _cat/indices on many managed clusters), and surfaces user-facing
// data stream names rather than their hidden .ds-* backing indices.
//
// Falls back to _cat/indices on any error from _resolve. The most common reason
// for resolve to fail is the API not existing on Elasticsearch versions older
// than 7.9; the fallback also covers transient errors and atypical permission
// configurations where cat is allowed but resolve is not.
func listAllIndexNames(ctx context.Context, info *es.DatasourceInfo, s *schemaSettings) ([]string, error) {
	timeout := s.IndicesTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	names, err := listIndicesViaResolve(ctx, info, s)
	if err == nil {
		return names, nil
	}
	resolveErr := err

	names, err = listIndicesViaCat(ctx, info, s)
	if err != nil {
		return nil, fmt.Errorf("list indices: resolve failed (%v); cat fallback failed: %w", resolveErr, err)
	}
	return names, nil
}

func isForbiddenOrUnauthorized(err error) bool {
	s := err.Error()
	return strings.Contains(s, "HTTP 403") || strings.Contains(s, "HTTP 401")
}

// filterAndSortIndices normalizes a raw list of names: trims whitespace, drops
// hidden names (those with a leading "."), deduplicates, sorts, and truncates
// to s.MaxIndices.
//
// Dedup matters because the resolve path concatenates indices, aliases and
// data streams — distinct ES concepts that nonetheless share a flat namespace
// and could in principle collide.
func filterAndSortIndices(names []string, s *schemaSettings) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !s.IncludeHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	max := s.MaxIndices
	if max <= 0 {
		max = defaultSchemaMaxIndices
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func truncateForErr(b []byte) string {
	const max = 256
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

// fieldCapsToColumns maps Elasticsearch field_caps JSON to schemads columns (merged types per field path).
//
// The _field_caps response shape is:
//
//	{ "indices": ["idx-a", ...], "fields": { "<name>": { "<es-type>": { ... } } } }
//
// "indices" is a string array and "fields" lives at the root — not nested per index.
func fieldCapsToColumns(fieldCapsJSON []byte, timeField string) ([]schemas.Column, error) {
	var root map[string]any
	if err := json.Unmarshal(fieldCapsJSON, &root); err != nil {
		return nil, err
	}

	fields, _ := root["fields"].(map[string]any)
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty field_caps response: no fields returned")
	}

	names := make([]string, 0, len(fields))
	for fname, fval := range fields {
		if _, ok := fval.(map[string]any); !ok {
			continue
		}
		names = append(names, fname)
	}
	sort.Strings(names)

	cols := make([]schemas.Column, 0, len(names))
	for _, fname := range names {
		if len(cols) >= maxFieldColumns {
			break
		}
		types := fields[fname].(map[string]any)
		col := columnFromMergedTypes(fname, types, fname == timeField)
		cols = append(cols, col)
	}
	return cols, nil
}

func columnFromMergedTypes(fieldName string, types map[string]any, isConfiguredTimeField bool) schemas.Column {
	var hasKeyword, hasText, hasDate, hasBool bool
	var hasInt, hasFloat, hasObject bool
	for tname := range types {
		switch tname {
		case "keyword", "ip", "version":
			hasKeyword = true
		case "text":
			hasText = true
		case "date", "date_nanos":
			hasDate = true
		case "boolean":
			hasBool = true
		case "long", "integer", "short", "byte":
			hasInt = true
		case "double", "float", "half_float", "scaled_float":
			hasFloat = true
		case "object", "nested":
			hasObject = true
		case "flattened":
			hasKeyword = true
		}
	}

	if hasObject && !hasKeyword && !hasText && !hasDate && !hasBool && !hasInt && !hasFloat {
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeJSON,
			Description: esTypeDescription(types),
		}
	}

	switch {
	case hasDate || isConfiguredTimeField:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeDatetime,
			Operators:   timeRangeOperators,
			Description: esTypeDescription(types),
		}
	case hasBool:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeBoolean,
			Operators:   equalityOperators,
			Description: esTypeDescription(types),
		}
	case hasInt:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeInt64,
			Operators:   append(append([]schemas.Operator{}, timeRangeOperators...), equalityOperators...),
			Description: esTypeDescription(types),
		}
	case hasFloat:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeFloat64,
			Operators:   append(append([]schemas.Operator{}, timeRangeOperators...), equalityOperators...),
			Description: esTypeDescription(types),
		}
	case hasKeyword && !hasText:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeString,
			Operators:   equalityOperators,
			Description: esTypeDescription(types),
		}
	case hasText:
		ops := searchOperators
		if hasKeyword {
			ops = append(ops, equalityOperators...)
		}
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeString,
			Operators:   ops,
			Description: esTypeDescription(types),
		}
	default:
		return schemas.Column{
			Name:        fieldName,
			Type:        schemas.ColumnTypeString,
			Operators:   equalityOperators,
			Description: esTypeDescription(types),
		}
	}
}

func esTypeDescription(types map[string]any) string {
	var names []string
	for n := range types {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return "Elasticsearch types: " + strings.Join(names, ", ")
}
