package elasticsearch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	schemas "github.com/grafana/schemads"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

const (
	dsAbstractionAppFeature = "dsAbstractionApp"
	// maxHistogramBuckets stays well within the ES default of 65,536.
	maxHistogramBuckets = 1000
	minInterval         = time.Second
)

// normalizeGrafanaSQLRequest rewrites dsabstraction queries (grafanaSql) into native Elasticsearch query JSON.
func normalizeGrafanaSQLRequest(logger log.Logger, ds *DataSource, req *backend.QueryDataRequest) *backend.QueryDataRequest {
	if req == nil || len(req.Queries) == 0 {
		return req
	}

	cfg := req.PluginContext.GrafanaConfig
	if cfg == nil {
		logger.Warn("grafanaConfig is not set, skipping grafanaSql normalization")
		return req
	}
	if !cfg.FeatureToggles().IsEnabled(dsAbstractionAppFeature) {
		return req
	}

	cf := ds.info.ConfiguredFields
	out := make([]backend.DataQuery, 0, len(req.Queries))
	for _, q := range req.Queries {
		var sq schemas.Query
		if err := json.Unmarshal(q.JSON, &sq); err != nil {
			out = append(out, q)
			continue
		}
		if !sq.GrafanaSql || sq.Table == "" {
			out = append(out, q)
			continue
		}

		index := resolveSQLIndex(&sq)
		if index == "" {
			logger.Warn("grafanaSql missing index target", "table", sq.Table)
			out = append(out, q)
			continue
		}

		refID := sq.RefID
		if refID == "" {
			refID = q.RefID
		}

		tr := mergeTimeRangeFromFilters(q.TimeRange, sq.Filters, cf)
		model, err := buildLuceneGrafanaSQL(refID, index, cf, sq.Filters)
		if err != nil {
			logger.Warn("grafanaSql normalization failed", "error", err, "table", sq.Table)
			out = append(out, q)
			continue
		}

		jsonBytes, err := json.Marshal(model)
		if err != nil {
			out = append(out, q)
			continue
		}

		interval := q.Interval
		maxDP := q.MaxDataPoints
		if interval <= 0 {
			interval = safeFallbackInterval(tr, maxDP)
		}

		out = append(out, backend.DataQuery{
			RefID:         refID,
			QueryType:     "lucene",
			TimeRange:     tr,
			Interval:      interval,
			MaxDataPoints: maxDP,
			JSON:          jsonBytes,
		})
	}

	return &backend.QueryDataRequest{
		PluginContext: req.PluginContext,
		Headers:       req.Headers,
		Queries:       out,
		Format:        req.Format,
	}
}

func resolveSQLIndex(sq *schemas.Query) string {
	if sq.Table == fallbackTableName {
		return tableParamAny(sq.TableParameterValues, tableParamIndex)
	}
	return strings.TrimSpace(sq.Table)
}

func tableParamAny(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func scalarNumber(v any) (string, bool) {
	switch t := v.(type) {
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case json.Number:
		return t.String(), true
	case int64:
		return strconv.FormatInt(t, 10), true
	case int:
		return strconv.Itoa(t), true
	default:
		return "", false
	}
}

func buildLuceneGrafanaSQL(refID, index string, cf es.ConfiguredFields, filters []schemas.ColumnFilter) (map[string]any, error) {
	model := map[string]any{
		"refId":     refID,
		"query":     "*",
		"queryType": "lucene",
		"index":     index,
		"metrics": []map[string]any{
			{
				"type":     "logs",
				"id":       "1",
				"settings": map[string]any{"limit": strconv.Itoa(defaultSize)},
			},
		},
		"bucketAggs": []any{},
	}

	bf := buildBoolFilters(cf, filters)
	if bf != nil {
		model["boolFilters"] = bf
	}
	return model, nil
}

// boolFilterClauseJSON is a JSON-serializable structured filter clause.
type boolFilterClauseJSON struct {
	Type   string         `json:"type"`
	Field  string         `json:"field"`
	Value  any            `json:"value,omitempty"`
	Values []any          `json:"values,omitempty"`
	Bounds map[string]any `json:"bounds,omitempty"`
}

// boolFiltersJSON is a JSON-serializable set of bool filter/must_not clauses.
type boolFiltersJSON struct {
	Filter  []boolFilterClauseJSON `json:"filter,omitempty"`
	MustNot []boolFilterClauseJSON `json:"mustNot,omitempty"`
}

// buildBoolFilters translates schemads column filters into structured bool
// filter clauses. Time-column filters are skipped (handled via time range).
func buildBoolFilters(cf es.ConfiguredFields, filters []schemas.ColumnFilter) *boolFiltersJSON {
	var musts []boolFilterClauseJSON
	var mustNots []boolFilterClauseJSON

	for _, f := range filters {
		if f.Name == "" || isTimeColumn(f.Name, cf) {
			continue
		}
		for _, cond := range f.Conditions {
			clause, negate := filterConditionClause(f.Name, cond)
			if clause == nil {
				continue
			}
			if negate {
				mustNots = append(mustNots, *clause)
			} else {
				musts = append(musts, *clause)
			}
		}
	}

	if len(musts) == 0 && len(mustNots) == 0 {
		return nil
	}
	return &boolFiltersJSON{Filter: musts, MustNot: mustNots}
}

func filterConditionClause(field string, cond schemas.FilterCondition) (*boolFilterClauseJSON, bool) {
	switch cond.Operator {
	case schemas.OperatorEquals:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "match_phrase", Field: field, Value: v}, false
		}

	case schemas.OperatorNotEquals:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "match_phrase", Field: field, Value: v}, true
		}

	case schemas.OperatorIn:
		vals := make([]any, 0, len(cond.Values))
		for _, v := range cond.Values {
			if cv := coerceFilterValue(v); cv != nil {
				vals = append(vals, cv)
			}
		}
		if len(vals) > 0 {
			return &boolFilterClauseJSON{Type: "terms", Field: field, Values: vals}, false
		}

	case schemas.OperatorGreaterThan:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "range", Field: field, Bounds: map[string]any{"gt": v}}, false
		}
	case schemas.OperatorGreaterThanOrEqual:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "range", Field: field, Bounds: map[string]any{"gte": v}}, false
		}
	case schemas.OperatorLessThan:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "range", Field: field, Bounds: map[string]any{"lt": v}}, false
		}
	case schemas.OperatorLessThanOrEqual:
		v := coerceFilterValue(cond.Value)
		if v != nil {
			return &boolFilterClauseJSON{Type: "range", Field: field, Bounds: map[string]any{"lte": v}}, false
		}

	case schemas.OperatorLike:
		if s, ok := scalarString(cond.Value); ok {
			pat := strings.Trim(s, "%")
			return &boolFilterClauseJSON{Type: "wildcard", Field: field, Value: "*" + pat + "*"}, false
		}
	}
	return nil, false
}

func coerceFilterValue(v any) any {
	if s, ok := scalarString(v); ok {
		return s
	}
	if n, ok := scalarNumber(v); ok {
		return n
	}
	return nil
}

func isTimeColumn(name string, cf es.ConfiguredFields) bool {
	return name == cf.TimeField || strings.EqualFold(name, "time")
}

func mergeTimeRangeFromFilters(tr backend.TimeRange, filters []schemas.ColumnFilter, cf es.ConfiguredFields) backend.TimeRange {
	var minT, maxT *time.Time
	for _, f := range filters {
		if !isTimeColumn(f.Name, cf) {
			continue
		}
		for _, c := range f.Conditions {
			ts := parseFilterTime(c.Value)
			if ts == nil {
				continue
			}
			switch c.Operator {
			case schemas.OperatorGreaterThan:
				t := ts.Add(time.Nanosecond)
				minT = pickMinTime(minT, &t)
			case schemas.OperatorGreaterThanOrEqual:
				minT = pickMinTime(minT, ts)
			case schemas.OperatorLessThan:
				t := ts.Add(-time.Nanosecond)
				maxT = pickMaxTime(maxT, &t)
			case schemas.OperatorLessThanOrEqual:
				maxT = pickMaxTime(maxT, ts)
			default:
				continue
			}
		}
	}
	if minT != nil {
		tr.From = *minT
	}
	if maxT != nil {
		tr.To = *maxT
	}
	return tr
}

func scalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case fmt.Stringer:
		return t.String(), true
	default:
		return "", false
	}
}

func parseFilterTime(v any) *time.Time {
	switch t := v.(type) {
	case time.Time:
		return &t
	case string:
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, t); err == nil {
				return &parsed
			}
		}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			u := time.UnixMilli(i).UTC()
			return &u
		}
	case float64:
		u := time.UnixMilli(int64(t)).UTC()
		return &u
	case int64:
		u := time.UnixMilli(t).UTC()
		return &u
	}
	return nil
}

func pickMinTime(cur *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return cur
	}
	if cur == nil || candidate.After(*cur) {
		return candidate
	}
	return cur
}

func pickMaxTime(cur *time.Time, candidate *time.Time) *time.Time {
	if candidate == nil {
		return cur
	}
	if cur == nil || candidate.Before(*cur) {
		return candidate
	}
	return cur
}

// safeFallbackInterval computes a reasonable date-histogram interval from the
// query time range when the caller did not supply one. This prevents the
// "too many buckets" error that occurs when the interval is zero.
func safeFallbackInterval(tr backend.TimeRange, maxDataPoints int64) time.Duration {
	span := tr.To.Sub(tr.From)
	if span <= 0 {
		return minInterval
	}
	buckets := int64(maxHistogramBuckets)
	if maxDataPoints > 0 && maxDataPoints < buckets {
		buckets = maxDataPoints
	}
	interval := time.Duration(span.Nanoseconds() / buckets)
	if interval < minInterval {
		interval = minInterval
	}
	return interval
}
