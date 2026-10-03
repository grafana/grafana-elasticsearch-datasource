package elasticsearch

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// esqlDataplaneLayout is the role of each result column: time, value, dimension,
// or extra (a non-numeric aggregate kept as a remainder field).
type esqlDataplaneLayout struct {
	timeIdx   int
	valueIdxs []int
	dimIdxs   []int
	extraIdxs []int
}

var esqlAliasRe = regexp.MustCompile("^(`[^`]+`|[A-Za-z_@][\\w@.]*)\\s*=[^=]")

var esqlNumericTypes = map[string]struct{}{
	"long": {}, "integer": {}, "short": {}, "byte": {}, "unsigned_long": {},
	"double": {}, "float": {}, "half_float": {}, "scaled_float": {},
	"counter_long": {}, "counter_integer": {}, "counter_double": {},
}

// processEsqlDataplaneMetricsResponse picks the dataplane kind from the columns:
// timeseries-multi with a time dimension, numeric-long without one, raw table when no column is numeric.
func processEsqlDataplaneMetricsResponse(response *es.EsqlResponse, target *Query) (*backend.DataResponse, error) {
	if response == nil || len(response.Columns) == 0 {
		return &backend.DataResponse{Frames: data.Frames{newEmptyMetricsFrame(target.RefID, data.FrameTypeTimeSeriesMulti)}}, nil
	}
	layout, ok := classifyEsqlDataplaneColumns(response.Columns, target.RawQuery)
	if !ok {
		return processEsqlRawDataResponse(response, target)
	}
	if layout.timeIdx == -1 {
		return &backend.DataResponse{Frames: buildEsqlNumericLongFrames(response, layout, target)}, nil
	}
	return &backend.DataResponse{Frames: buildEsqlTimeSeriesFrames(response, layout, target)}, nil
}

// classifyEsqlDataplaneColumns reads the last STATS command: its BY keys are the
// dimensions, its aggregates are values (numeric) or extras, and a column it does
// not name (renamed or derived later) is a value when numeric, else a dimension.
// Without a STATS command the layout falls back to column types.
func classifyEsqlDataplaneColumns(columns []es.EsqlColumn, rawQuery string) (esqlDataplaneLayout, bool) {
	aggs, keys, hasStats := esqlStatsClauses(rawQuery)
	if !hasStats {
		return classifyEsqlColumnsByType(columns)
	}
	isKey, unmatchedKeys := esqlMatchColumns(columns, keys)
	isAgg, _ := esqlMatchColumns(columns, aggs)
	layout := esqlDataplaneLayout{timeIdx: -1}
	for i, col := range columns {
		if _, key := isKey[i]; key && isEsqlDateType(col.Type) {
			layout.timeIdx = i
			break
		}
	}
	if layout.timeIdx == -1 && unmatchedKeys > 0 {
		named := maps.Clone(isKey)
		maps.Copy(named, isAgg)
		layout.timeIdx = esqlFallbackTimeIdx(columns, named)
	}
	for i, col := range columns {
		_, key := isKey[i]
		_, agg := isAgg[i]
		switch {
		case i == layout.timeIdx:
		case key:
			layout.dimIdxs = append(layout.dimIdxs, i)
		case isEsqlNumericType(col.Type):
			layout.valueIdxs = append(layout.valueIdxs, i)
		case agg:
			layout.extraIdxs = append(layout.extraIdxs, i)
		default:
			layout.dimIdxs = append(layout.dimIdxs, i)
		}
	}
	return layout, len(layout.valueIdxs) > 0
}

// classifyEsqlColumnsByType mirrors the legacy classification for results without
// a STATS command (PROMQL): the bucket date column is time, the first numeric
// column the only value, everything else a dimension.
func classifyEsqlColumnsByType(columns []es.EsqlColumn) (esqlDataplaneLayout, bool) {
	layout := esqlDataplaneLayout{timeIdx: esqlFallbackTimeIdx(columns, nil)}
	for i, col := range columns {
		switch {
		case i == layout.timeIdx:
		case len(layout.valueIdxs) == 0 && isEsqlNumericType(col.Type):
			layout.valueIdxs = append(layout.valueIdxs, i)
		default:
			layout.dimIdxs = append(layout.dimIdxs, i)
		}
	}
	return layout, len(layout.valueIdxs) > 0
}

// esqlFallbackTimeIdx returns the date column most likely to be the bucket key:
// one named BUCKET(... or step, else the last date column not already a dimension,
// since STATS emits BY keys after the aggregates.
func esqlFallbackTimeIdx(columns []es.EsqlColumn, exclude map[int]struct{}) int {
	timeIdx := -1
	for i, col := range columns {
		if _, skip := exclude[i]; skip || !isEsqlDateType(col.Type) {
			continue
		}
		if looksLikeEsqlBucket(col.Name) {
			return i
		}
		timeIdx = i
	}
	return timeIdx
}

func looksLikeEsqlBucket(name string) bool {
	upper := strings.ToUpper(strings.TrimSpace(name))
	return strings.HasPrefix(upper, "BUCKET(") || upper == "STEP"
}

func isEsqlDateType(t string) bool {
	return t == "date" || t == "date_nanos"
}

func isEsqlNumericType(t string) bool {
	_, ok := esqlNumericTypes[t]
	return ok
}

// esqlMatchColumns maps each named expression to its result column and counts
// the names a later command renamed or dropped.
func esqlMatchColumns(columns []es.EsqlColumn, names []string) (map[int]struct{}, int) {
	matched := make(map[int]struct{}, len(names))
	unmatched := 0
	for _, name := range names {
		idx := slices.IndexFunc(columns, func(col es.EsqlColumn) bool { return esqlNamesEqual(col.Name, name) })
		if idx == -1 {
			unmatched++
			continue
		}
		matched[idx] = struct{}{}
	}
	return matched, unmatched
}

// esqlStatsClauses returns the aggregates and BY keys of the last STATS command
// as column names (the alias of `name = expression`, else the expression text,
// which is how ES|QL names a column); hasStats is false without a STATS command.
func esqlStatsClauses(query string) (aggs, keys []string, hasStats bool) {
	var stats string
	for _, command := range splitEsqlTopLevel(query, '|') {
		if words := strings.Fields(command); len(words) > 0 && strings.EqualFold(words[0], esqlStatsCommand) {
			stats = strings.TrimSpace(command)[len(esqlStatsCommand):]
		}
	}
	if stats == "" {
		return nil, nil, false
	}
	aggClause, byClause := esqlSplitBy(stats)
	return esqlColumnNames(aggClause), esqlColumnNames(byClause), true
}

// esqlStatsByKeys returns the BY keys of the last STATS command; see esqlStatsClauses.
func esqlStatsByKeys(query string) (keys []string, hasStats bool) {
	_, keys, hasStats = esqlStatsClauses(query)
	return keys, hasStats
}

func esqlColumnNames(clause string) []string {
	var names []string
	for _, part := range splitEsqlTopLevel(clause, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m := esqlAliasRe.FindStringSubmatch(part); m != nil {
			part = m[1]
		}
		names = append(names, strings.Trim(part, "`"))
	}
	return names
}

// esqlSplitBy splits a STATS command body at its top-level BY keyword.
func esqlSplitBy(stats string) (aggClause, byClause string) {
	depth, quote := 0, byte(0)
	for i := 0; i < len(stats); i++ {
		c := stats[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && i+1 < len(stats) && strings.EqualFold(stats[i:i+2], "BY") && isEsqlBoundary(stats, i-1) && isEsqlBoundary(stats, i+2):
			return stats[:i], stats[i+2:]
		}
	}
	return stats, ""
}

func isEsqlBoundary(s string, i int) bool {
	return i < 0 || i >= len(s) || unicode.IsSpace(rune(s[i]))
}

// splitEsqlTopLevel splits s at sep outside parentheses, brackets, quoted
// strings and backtick identifiers.
func splitEsqlTopLevel(s string, sep byte) []string {
	var parts []string
	depth, quote, start := 0, byte(0), 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case c == sep && depth == 0:
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return append(parts, s[start:])
}

// esqlNamesEqual compares a result column name with a BY key, ignoring case,
// surrounding backticks and whitespace differences inside expressions.
func esqlNamesEqual(a, b string) bool {
	norm := func(s string) string {
		return strings.Join(strings.Fields(strings.Trim(s, "`")), " ")
	}
	return strings.EqualFold(norm(a), norm(b))
}

// esqlSeries accumulates one breakdown group's rows.
type esqlSeries struct {
	labels data.Labels
	times  []time.Time
	values [][]*float64
	extras [][]string
}

// buildEsqlTimeSeriesFrames emits one timeseries-multi frame per breakdown
// group and value column, with rows ordered by time.
func buildEsqlTimeSeriesFrames(response *es.EsqlResponse, layout esqlDataplaneLayout, target *Query) data.Frames {
	groups := map[string]*esqlSeries{}
	var order []string
	for _, row := range response.Values {
		if layout.timeIdx >= len(row) {
			continue
		}
		ts, ok := parseEsqlDateTime(row[layout.timeIdx])
		if !ok {
			continue
		}
		labels, key := esqlRowDimensions(response.Columns, row, layout.dimIdxs)
		series, exists := groups[key]
		if !exists {
			series = &esqlSeries{labels: labels, values: make([][]*float64, len(layout.valueIdxs)), extras: make([][]string, len(layout.extraIdxs))}
			groups[key] = series
			order = append(order, key)
		}
		series.times = append(series.times, ts)
		for vi, colIdx := range layout.valueIdxs {
			series.values[vi] = append(series.values[vi], esqlNumericCell(row, colIdx))
		}
		for ei, colIdx := range layout.extraIdxs {
			cell := ""
			if colIdx < len(row) {
				cell = esqlLabelValue(row[colIdx])
			}
			series.extras[ei] = append(series.extras[ei], cell)
		}
	}
	if len(order) == 0 {
		return data.Frames{newEmptyMetricsFrame(target.RefID, data.FrameTypeTimeSeriesMulti)}
	}

	frames := make(data.Frames, 0, len(order)*len(layout.valueIdxs))
	for _, key := range order {
		series := groups[key]
		for vi, colIdx := range layout.valueIdxs {
			name := response.Columns[colIdx].Name
			valueField := data.NewField(name, maps.Clone(series.labels), series.values[vi])
			legend := esqlSeriesLegend(series.labels, name, len(layout.valueIdxs) > 1)
			valueField.Config = &data.FieldConfig{DisplayNameFromDS: legend}
			frame := data.NewFrame(legend, data.NewField(data.TimeSeriesTimeFieldName, nil, slices.Clone(series.times)), valueField)
			for ei, colIdx := range layout.extraIdxs {
				frame.Fields = append(frame.Fields, data.NewField(response.Columns[colIdx].Name, nil, slices.Clone(series.extras[ei])))
			}
			sortRowsByTime(frame, 0)
			setMetricsFrameMeta(frame, data.FrameTypeTimeSeriesMulti)
			frames = append(frames, frame)
		}
	}
	return frames
}

// buildEsqlNumericLongFrames emits the single numeric-long frame of a grouped
// result without a time dimension, keeping the response column order.
func buildEsqlNumericLongFrames(response *es.EsqlResponse, layout esqlDataplaneLayout, target *Query) data.Frames {
	rows := len(response.Values)
	if rows == 0 {
		return data.Frames{newEmptyMetricsFrame(target.RefID, data.FrameTypeNumericLong)}
	}
	isValue := make(map[int]struct{}, len(layout.valueIdxs))
	for _, i := range layout.valueIdxs {
		isValue[i] = struct{}{}
	}
	isFilterable := true
	fields := make([]*data.Field, 0, len(response.Columns))
	for colIdx, col := range response.Columns {
		var field *data.Field
		if _, value := isValue[colIdx]; value {
			values := make([]*float64, rows)
			for r, row := range response.Values {
				values[r] = esqlNumericCell(row, colIdx)
			}
			field = data.NewField(col.Name, nil, values)
		} else {
			values := make([]string, rows)
			for r, row := range response.Values {
				if colIdx < len(row) {
					values[r] = esqlLabelValue(row[colIdx])
				}
			}
			field = data.NewField(col.Name, nil, values)
		}
		field.Config = &data.FieldConfig{Filterable: &isFilterable}
		fields = append(fields, field)
	}
	frame := data.NewFrame("", fields...)
	frame.RefID = target.RefID
	setMetricsFrameMeta(frame, data.FrameTypeNumericLong)
	return data.Frames{frame}
}

// esqlRowDimensions returns the row's dimension labels and the grouping key.
func esqlRowDimensions(columns []es.EsqlColumn, row []interface{}, dimIdxs []int) (data.Labels, string) {
	if len(dimIdxs) == 0 {
		return nil, ""
	}
	labels := make(data.Labels, len(dimIdxs))
	parts := make([]string, len(dimIdxs))
	for i, colIdx := range dimIdxs {
		value := ""
		if colIdx < len(row) {
			value = esqlLabelValue(row[colIdx])
		}
		labels[columns[colIdx].Name] = value
		parts[i] = value
	}
	return labels, strings.Join(parts, "\x00")
}

// esqlLabelValue renders a dimension cell as a label value: numbers without an
// exponent, booleans as true/false and multivalued cells joined with commas.
func esqlLabelValue(v interface{}) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	case []interface{}:
		parts := make([]string, len(value))
		for i, item := range value {
			parts[i] = esqlLabelValue(item)
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprintf("%v", value)
	}
}

func esqlNumericCell(row []interface{}, colIdx int) *float64 {
	if colIdx >= len(row) {
		return nil
	}
	if f, ok := toFloat64(row[colIdx]); ok {
		return &f
	}
	return nil
}

// esqlSeriesLegend mirrors the DSL legend: the dimension values in key order,
// followed by the value column name when the query has several value columns.
func esqlSeriesLegend(labels data.Labels, name string, multiValue bool) string {
	if len(labels) == 0 {
		return name
	}
	legend := strings.Join(getSortedLabelValues(labels), " ")
	if multiValue {
		legend += " " + name
	}
	return legend
}
