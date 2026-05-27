package elasticsearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// processEsqlLogsResponse processes ES|QL response for logs queries
// Similar to how logs are processed in logs_response_processor.go
func processEsqlLogsResponse(response *es.EsqlResponse, target *Query, configuredFields es.ConfiguredFields) (*backend.DataResponse, error) {
	if response == nil || len(response.Columns) == 0 {
		return &backend.DataResponse{
			Frames: []*data.Frame{data.NewFrame(target.RefID)},
		}, nil
	}

	// Build column index map for quick lookup
	colIndexMap := make(map[string]int)
	for i, col := range response.Columns {
		colIndexMap[col.Name] = i
	}

	// Convert ES|QL rows to document maps (similar to how logs processor handles hits)
	docs := make([]map[string]interface{}, len(response.Values))
	propNames := make(map[string]bool)

	for rowIdx, row := range response.Values {
		doc := make(map[string]interface{})

		for colIdx, col := range response.Columns {
			if colIdx < len(row) && row[colIdx] != nil {
				doc[col.Name] = row[colIdx]
				propNames[col.Name] = true

				// Map configured log level field to "level"
				if configuredFields.LogLevelField != "" && col.Name == configuredFields.LogLevelField {
					doc["level"] = row[colIdx]
				}
			}
		}

		// Create a unique ID if not present
		if _, hasID := doc["id"]; !hasID {
			if _, hasUnderscoreID := doc["_id"]; !hasUnderscoreID {
				doc["id"] = fmt.Sprintf("esql-row-%d", rowIdx)
			}
		}

		// Create _source JSON string for logs panel compatibility
		sourceBytes, _ := json.Marshal(doc)
		doc["_source"] = string(sourceBytes)

		docs[rowIdx] = doc
	}

	sortedPropNames := sortPropNames(propNames, configuredFields, true)
	fields := processDocsToDataFrameFields(docs, sortedPropNames, configuredFields)

	frame := data.NewFrame(target.RefID, fields...)
	setPreferredVisType(frame, data.VisTypeLogs)

	// Set logs metadata
	limit := defaultSize
	if len(target.Metrics) > 0 {
		limit = stringToIntWithDefaultValue(target.Metrics[0].Settings.Get("limit").MustString(), defaultSize)
	}
	setLogsCustomMeta(frame, map[string]bool{}, limit, len(response.Values))

	return &backend.DataResponse{
		Frames: []*data.Frame{frame},
	}, nil
}

// processEsqlRawDataResponse processes ES|QL response for raw_data queries (table format)
// This is the default processing mode for ES|QL responses
// It uses ES|QL column type metadata to properly handle datetime columns
func processEsqlRawDataResponse(response *es.EsqlResponse, target *Query) (*backend.DataResponse, error) {
	if response == nil || len(response.Columns) == 0 {
		return &backend.DataResponse{
			Frames: []*data.Frame{data.NewFrame(target.RefID)},
		}, nil
	}

	// Create fields directly from ES|QL columns using type metadata
	fields := processEsqlColumnsToFields(response)

	frame := data.NewFrame(target.RefID, fields...)
	setPreferredVisType(frame, data.VisTypeTable)

	return &backend.DataResponse{
		Frames: []*data.Frame{frame},
	}, nil
}

// esqlColumnLayout holds the indices of time, value, and breakdown columns
// identified from an ES|QL response.
type esqlColumnLayout struct {
	timeColIdx       int
	valueColIdx      int
	breakdownColIdxs []int
}

// classifyEsqlColumns inspects ES|QL column metadata and returns the indices
// of the first date column (time), the first numeric column (value), and all
// remaining columns (breakdown dimensions).
func classifyEsqlColumns(columns []es.EsqlColumn) esqlColumnLayout {
	layout := esqlColumnLayout{timeColIdx: -1, valueColIdx: -1}

	for i, col := range columns {
		switch col.Type {
		case "date", "date_nanos":
			if layout.timeColIdx == -1 {
				layout.timeColIdx = i
			}
		case "long", "integer", "short", "byte", "double", "float", "half_float", "scaled_float":
			if layout.valueColIdx == -1 {
				layout.valueColIdx = i
			}
		}
	}

	for i := range columns {
		if i != layout.timeColIdx && i != layout.valueColIdx {
			layout.breakdownColIdxs = append(layout.breakdownColIdxs, i)
		}
	}

	return layout
}

// buildEsqlSingleSeriesFrame creates a single time series frame from rows
// when there are no breakdown columns.
func buildEsqlSingleSeriesFrame(response *es.EsqlResponse, layout esqlColumnLayout, metricName string) *data.Frame {
	timeVector := make([]time.Time, 0, len(response.Values))
	valueVector := make([]*float64, 0, len(response.Values))

	for _, row := range response.Values {
		if layout.timeColIdx >= len(row) {
			continue
		}
		ts, ok := parseEsqlDateTime(row[layout.timeColIdx])
		if !ok {
			continue
		}
		var value *float64
		if layout.valueColIdx < len(row) && row[layout.valueColIdx] != nil {
			if v, ok := toFloat64(row[layout.valueColIdx]); ok {
				value = &v
			}
		}
		timeVector = append(timeVector, ts)
		valueVector = append(valueVector, value)
	}

	if len(timeVector) == 0 {
		return nil
	}

	frame := newTimeSeriesFrame(timeVector, nil, valueVector)
	frame.Name = metricName
	return frame
}

// esqlSeriesData holds accumulated time/value vectors and labels for a single
// breakdown group.
type esqlSeriesData struct {
	timeVector  []time.Time
	valueVector []*float64
	labels      map[string]string
}

// buildEsqlMultiSeriesFrames groups rows by breakdown column values and returns
// one time series frame per unique combination of breakdown values.
func buildEsqlMultiSeriesFrames(response *es.EsqlResponse, layout esqlColumnLayout, metricName string) []*data.Frame {
	seriesMap := make(map[string]*esqlSeriesData)
	var seriesOrder []string

	for _, row := range response.Values {
		if layout.timeColIdx >= len(row) {
			continue
		}
		ts, ok := parseEsqlDateTime(row[layout.timeColIdx])
		if !ok {
			continue
		}

		// Build the group key and labels from breakdown columns
		labels := make(map[string]string, len(layout.breakdownColIdxs))
		keyParts := make([]string, len(layout.breakdownColIdxs))
		for i, colIdx := range layout.breakdownColIdxs {
			val := ""
			if colIdx < len(row) && row[colIdx] != nil {
				val = fmt.Sprintf("%v", row[colIdx])
			}
			labels[response.Columns[colIdx].Name] = val
			keyParts[i] = val
		}
		key := strings.Join(keyParts, "|||")

		var value *float64
		if layout.valueColIdx < len(row) && row[layout.valueColIdx] != nil {
			if v, ok := toFloat64(row[layout.valueColIdx]); ok {
				value = &v
			}
		}

		sd, exists := seriesMap[key]
		if !exists {
			sd = &esqlSeriesData{labels: labels}
			seriesMap[key] = sd
			seriesOrder = append(seriesOrder, key)
		}
		sd.timeVector = append(sd.timeVector, ts)
		sd.valueVector = append(sd.valueVector, value)
	}

	if len(seriesOrder) == 0 {
		return nil
	}

	frames := make([]*data.Frame, 0, len(seriesOrder))
	for _, key := range seriesOrder {
		sd := seriesMap[key]
		frame := newTimeSeriesFrame(sd.timeVector, sd.labels, sd.valueVector)
		frame.Name = metricName
		frames = append(frames, frame)
	}
	return frames
}

// processEsqlMetricsResponse processes ES|QL response for metrics queries.
// It maps a time column + numeric value column to a timeseries-multi frame
// to match the shape returned by regular/raw DSL metrics queries.
// When breakdown columns are present (columns that are neither time nor numeric),
// rows are grouped by unique breakdown values and separate frames are created.
func processEsqlMetricsResponse(response *es.EsqlResponse, target *Query) (*backend.DataResponse, error) {
	if !hasEsqlStatsCommand(target.RawQuery) {
		return &backend.DataResponse{}, nil
	}

	if response == nil || len(response.Columns) == 0 {
		return &backend.DataResponse{
			Frames: []*data.Frame{data.NewFrame(target.RefID)},
		}, nil
	}

	layout := classifyEsqlColumns(response.Columns)

	if layout.timeColIdx == -1 || layout.valueColIdx == -1 {
		return processEsqlRawDataResponse(response, target)
	}

	metricName := getMetricName(countType)
	if len(target.Metrics) > 0 && target.Metrics[0] != nil && target.Metrics[0].Type != "" {
		metricName = getMetricName(target.Metrics[0].Type)
	}

	if len(layout.breakdownColIdxs) == 0 {
		frame := buildEsqlSingleSeriesFrame(response, layout, metricName)
		if frame == nil {
			return processEsqlRawDataResponse(response, target)
		}
		return &backend.DataResponse{
			Frames: []*data.Frame{frame},
		}, nil
	}

	frames := buildEsqlMultiSeriesFrames(response, layout, metricName)
	if frames == nil {
		return processEsqlRawDataResponse(response, target)
	}
	return &backend.DataResponse{
		Frames: frames,
	}, nil
}

func hasEsqlStatsCommand(query string) bool {
	for _, token := range strings.Fields(strings.ToUpper(query)) {
		if strings.Trim(token, "|,;") == "STATS" {
			return true
		}
	}
	return false
}

// processEsqlColumnsToFields creates data frame fields from ES|QL columns using type metadata
// This properly handles datetime columns based on ES|QL's column type information
func processEsqlColumnsToFields(response *es.EsqlResponse) []*data.Field {
	fields := make([]*data.Field, len(response.Columns))
	isFilterable := true

	for colIdx, col := range response.Columns {
		switch col.Type {
		case "date", "date_nanos":
			// Handle datetime columns - parse ISO 8601 strings to time.Time
			// ES|QL uses "date" for standard datetime and "date_nanos" for nanosecond precision
			timeVector := make([]*time.Time, len(response.Values))
			for rowIdx, row := range response.Values {
				if colIdx < len(row) && row[colIdx] != nil {
					if t, ok := parseEsqlDateTime(row[colIdx]); ok {
						timeVector[rowIdx] = &t
					}
				}
			}
			field := data.NewField(col.Name, nil, timeVector)
			field.Config = &data.FieldConfig{Filterable: &isFilterable}
			fields[colIdx] = field

		case "long", "integer", "short", "byte":
			// Handle integer columns
			intVector := make([]*int64, len(response.Values))
			for rowIdx, row := range response.Values {
				if colIdx < len(row) && row[colIdx] != nil {
					if v, ok := toInt64(row[colIdx]); ok {
						intVector[rowIdx] = &v
					}
				}
			}
			field := data.NewField(col.Name, nil, intVector)
			field.Config = &data.FieldConfig{Filterable: &isFilterable}
			fields[colIdx] = field

		case "double", "float", "half_float", "scaled_float":
			// Handle float columns
			floatVector := make([]*float64, len(response.Values))
			for rowIdx, row := range response.Values {
				if colIdx < len(row) && row[colIdx] != nil {
					if v, ok := toFloat64(row[colIdx]); ok {
						floatVector[rowIdx] = &v
					}
				}
			}
			field := data.NewField(col.Name, nil, floatVector)
			field.Config = &data.FieldConfig{Filterable: &isFilterable}
			fields[colIdx] = field

		case "boolean":
			// Handle boolean columns
			boolVector := make([]*bool, len(response.Values))
			for rowIdx, row := range response.Values {
				if colIdx < len(row) && row[colIdx] != nil {
					if v, ok := row[colIdx].(bool); ok {
						boolVector[rowIdx] = &v
					}
				}
			}
			field := data.NewField(col.Name, nil, boolVector)
			field.Config = &data.FieldConfig{Filterable: &isFilterable}
			fields[colIdx] = field

		default:
			// Default to string for all other types
			stringVector := make([]*string, len(response.Values))
			for rowIdx, row := range response.Values {
				if colIdx < len(row) && row[colIdx] != nil {
					if v, ok := toString(row[colIdx]); ok {
						stringVector[rowIdx] = &v
					}
				}
			}
			field := data.NewField(col.Name, nil, stringVector)
			field.Config = &data.FieldConfig{Filterable: &isFilterable}
			fields[colIdx] = field
		}
	}

	return fields
}

// parseEsqlDateTime parses a datetime value from ES|QL response
func parseEsqlDateTime(value interface{}) (time.Time, bool) {
	switch v := value.(type) {
	case string:
		// Try parsing as ISO 8601 format (RFC3339)
		t, err := time.Parse(time.RFC3339, v)
		if err == nil {
			return t, true
		}
		// Try parsing as ISO 8601 with milliseconds
		t, err = time.Parse("2006-01-02T15:04:05.000Z", v)
		if err == nil {
			return t, true
		}
		// Try parsing without timezone
		t, err = time.Parse("2006-01-02T15:04:05", v)
		if err == nil {
			return t, true
		}
		// Try parsing with space separator (2006-01-02 15:04:05)
		t, err = time.Parse("2006-01-02 15:04:05", v)
		if err == nil {
			return t, true
		}
		return time.Time{}, false
	case float64:
		// Assume epoch milliseconds
		return time.UnixMilli(int64(v)), true
	case int64:
		// Assume epoch milliseconds
		return time.UnixMilli(v), true
	default:
		return time.Time{}, false
	}
}

// toInt64 converts a value to int64
func toInt64(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	default:
		return 0, false
	}
}

// toFloat64 converts a value to float64
func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}

// toString converts a value to string
func toString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case float64:
		return fmt.Sprintf("%v", v), true
	case int64:
		return fmt.Sprintf("%d", v), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		return fmt.Sprintf("%v", value), true
	}
}

// processEsqlRawDocumentResponse processes ES|QL response for raw_document queries (JSON format)
// Each row is returned as a complete JSON document
func processEsqlRawDocumentResponse(response *es.EsqlResponse, target *Query) (*backend.DataResponse, error) {
	if response == nil || len(response.Columns) == 0 {
		return &backend.DataResponse{
			Frames: []*data.Frame{data.NewFrame(target.RefID)},
		}, nil
	}

	// Convert each row to a JSON document
	fieldVector := make([]*json.RawMessage, len(response.Values))

	for rowIdx, row := range response.Values {
		doc := make(map[string]interface{})

		for colIdx, col := range response.Columns {
			if colIdx < len(row) {
				doc[col.Name] = row[colIdx]
			}
		}

		bytes, err := json.Marshal(doc)
		if err != nil {
			continue
		}
		value := json.RawMessage(bytes)
		fieldVector[rowIdx] = &value
	}

	isFilterable := true
	field := data.NewField(target.RefID, nil, fieldVector)
	field.Config = &data.FieldConfig{Filterable: &isFilterable}

	frame := data.NewFrame(target.RefID, field)
	setPreferredVisType(frame, data.VisTypeTable)

	return &backend.DataResponse{
		Frames: []*data.Frame{frame},
	}, nil
}
