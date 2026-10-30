package elasticsearch

import (
	"strconv"

	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

// setFloatPath converts a string value at the specified path to float64
func setFloatPath(settings *simplejson.Json, path ...string) {
	if stringValue, err := settings.GetPath(path...).String(); err == nil {
		if value, err := strconv.ParseFloat(stringValue, 64); err == nil {
			settings.SetPath(path, value)
		}
	}
}

// setIntPath converts a string value at the specified path to int64
func setIntPath(settings *simplejson.Json, path ...string) {
	if stringValue, err := settings.GetPath(path...).String(); err == nil {
		if value, err := strconv.ParseInt(stringValue, 10, 64); err == nil {
			settings.SetPath(path, value)
		}
	}
}

// generateSettingsForDSL casts values to float when required by Elastic's query DSL for MetricAgg
func (metricAggregation MetricAgg) generateSettingsForDSL() map[string]any {
	switch metricAggregation.Type {
	case "serial_diff":
		setFloatPath(metricAggregation.Settings, "lag")
	}

	if isMetricAggregationWithInlineScriptSupport(metricAggregation.Type) {
		scriptValue, err := metricAggregation.Settings.GetPath("script").String()
		if err != nil {
			// the script is stored using the old format : `script:{inline: "value"}` or is not set
			scriptValue, err = metricAggregation.Settings.GetPath("script", "inline").String()
		}

		if err == nil {
			metricAggregation.Settings.SetPath([]string{"script"}, scriptValue)
		}
	}

	return metricAggregation.Settings.MustMap()
}

// generateSettingsForDSL converts bucket aggregation settings to DSL format
func (bucketAgg BucketAgg) generateSettingsForDSL() map[string]any {
	setIntPath(bucketAgg.Settings, "min_doc_count")

	return bucketAgg.Settings.MustMap()
}

// stringToIntWithDefaultValue converts a string to int with a default fallback value
func stringToIntWithDefaultValue(valueStr string, defaultValue int) int {
	// In our case, 0 is not a valid value, so an unparseable or zero value both
	// fall back to the default.
	value, err := strconv.Atoi(valueStr)
	if err != nil || value == 0 {
		return defaultValue
	}
	return value
}

// stringToFloatWithDefaultValue converts a string to float64 with a default fallback value
func stringToFloatWithDefaultValue(valueStr string, defaultValue float64) float64 {
	// In our case, 0 is not a valid value, so an unparseable or zero value both
	// fall back to the default.
	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil || value == 0 {
		return defaultValue
	}
	return value
}
