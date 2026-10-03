package elasticsearch

import (
	"sort"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// nameFields applies naming logic to data frame fields based on query configuration
func nameFields(queryResult backend.DataResponse, target *Query, keepLabelsInResponse bool, dataplane bool) {
	set := make(map[string]struct{})
	frames := queryResult.Frames
	for _, v := range frames {
		for _, vv := range v.Fields {
			if metricType, exists := vv.Labels["metric"]; exists {
				set[metricType] = struct{}{}
			}
		}
	}
	metricTypeCount := len(set)
	seen := make(map[string]string)
	for _, frame := range frames {
		if frame.Meta != nil && frame.Meta.Type == data.FrameTypeTimeSeriesMulti {
			// if it is a time-series-multi, it means it has two columns, one is "time",
			// another is "number"
			valueField := frame.Fields[1]
			if dataplane {
				nameDataplaneSeries(frame, valueField, target, metricTypeCount, seen)
				continue
			}
			fieldName := getFieldName(*valueField, target, metricTypeCount)
			// If we  need to keep the labels in the response, to prevent duplication in names and to keep
			// backward compatibility with alerting and expressions we use DisplayNameFromDS
			if keepLabelsInResponse {
				if valueField.Config == nil {
					valueField.Config = &data.FieldConfig{}
				}
				valueField.Config.DisplayNameFromDS = fieldName
				// If we don't need to keep labels (how frontend mode worked), we use frame.Name and remove labels
			} else {
				valueField.Labels = nil
				frame.Name = fieldName
			}
		}
	}
}

// getFieldName generates a field name based on the data field and target configuration
func getFieldName(dataField data.Field, target *Query, metricTypeCount int) string {
	metricType := dataField.Labels["metric"]
	metricName := getMetricName(metricType)
	itemName := metricItemName(dataField.Labels, target)
	delete(dataField.Labels, "metric")

	field := ""
	if v, ok := dataField.Labels["field"]; ok {
		field = v
		delete(dataField.Labels, "field")
	}

	if target.Alias != "" {
		frameName := target.Alias

		subMatches := aliasPatternRegex.FindAllStringSubmatch(target.Alias, -1)
		for _, subMatch := range subMatches {
			group := subMatch[0]

			if len(subMatch) > 1 {
				group = subMatch[1]
			}

			if strings.HasPrefix(group, "term ") {
				frameName = strings.Replace(frameName, subMatch[0], dataField.Labels[group[5:]], 1)
			}
			if v, ok := dataField.Labels[group]; ok {
				frameName = strings.Replace(frameName, subMatch[0], v, 1)
			}
			if group == "metric" {
				frameName = strings.Replace(frameName, subMatch[0], metricName, 1)
			}
			if group == "field" {
				frameName = strings.Replace(frameName, subMatch[0], field, 1)
			}
		}

		return frameName
	}
	delete(dataField.Labels, "metricId")

	if len(dataField.Labels) == 0 {
		return itemName
	}

	name := ""
	for _, v := range getSortedLabelValues(dataField.Labels) {
		name += v + " "
	}

	if metricTypeCount == 1 {
		return strings.TrimSpace(name)
	}

	return strings.TrimSpace(name) + " " + itemName
}

// metricItemName returns the alias-independent item name built from the
// internal metric, field and metricId labels.
func metricItemName(labels data.Labels, target *Query) string {
	metricType := labels["metric"]
	field := labels["field"]
	metricID := labels["metricId"]
	metricName := getMetricName(metricType)

	switch {
	case isPipelineAgg(metricType):
		if isPipelineAggWithMultipleBucketPaths(metricType) {
			for _, metric := range target.Metrics {
				if metric.ID != metricID {
					continue
				}
				metricName = metric.Settings.Get("script").MustString()
				for name, pipelineAgg := range metric.PipelineVariables {
					for _, m := range target.Metrics {
						if m.ID == pipelineAgg {
							metricName = strings.ReplaceAll(metricName, "params."+name, describeMetric(m.Type, m.Field))
						}
					}
				}
			}
		} else if field != "" {
			found := false
			for _, metric := range target.Metrics {
				if metric.ID == field {
					metricName += " " + describeMetric(metric.Type, metric.Field)
					found = true
				}
			}
			if !found {
				metricName = "Unset"
			}
		}
	case isSiblingPipelineAgg(metricType):
		for _, metric := range target.Metrics {
			if metric.ID != metricID {
				continue
			}
			inner := metric.Settings.Get("metric").MustString()
			if _, ok := validSiblingInnerStats[inner]; !ok {
				inner = defaultSiblingInnerStat
			}
			metricName = siblingAggOuterName[metricType] + " of " + getMetricName(inner) + " " + field
			if groupBy := metric.Settings.Get("groupBy").MustString(); groupBy != "" {
				metricName += " per " + groupBy
			}
			break
		}
	case field != "":
		metricName += " " + field
	}

	return metricName
}

// getSortedLabelValues sorts label keys and returns the label values in sorted order
func getSortedLabelValues(labels data.Labels) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = labels[key]
	}

	return values
}

// getMetricName returns the display name for a metric type
func getMetricName(metric string) string {
	if text, ok := metricAggType[metric]; ok {
		return text
	}

	if text, ok := extendedStats[metric]; ok {
		return text
	}

	return metric
}
