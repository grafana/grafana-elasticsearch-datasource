package elasticsearch

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

// metricsResponseProcessor handles processing of metrics query responses
type metricsResponseProcessor struct {
	dataplane bool
}

// newMetricsResponseProcessor creates a new metrics response processor
func newMetricsResponseProcessor(dataplane bool) *metricsResponseProcessor {
	return &metricsResponseProcessor{dataplane: dataplane}
}

// processBuckets processes aggregation buckets recursively
func (p *metricsResponseProcessor) processBuckets(aggs map[string]interface{}, target *Query,
	queryResult *backend.DataResponse, props map[string]string, depth int) error {
	var err error
	maxDepth := len(target.BucketAggs) - 1

	aggIDs := make([]string, 0)
	for k := range aggs {
		aggIDs = append(aggIDs, k)
	}
	sort.Strings(aggIDs)
	for _, aggID := range aggIDs {
		v := aggs[aggID]
		aggDef, _ := findAgg(target, aggID)
		esAgg := simplejson.NewFromAny(v)
		if aggDef == nil {
			continue
		}
		if aggDef.Type == nestedType {
			err = p.processBuckets(esAgg.MustMap(), target, queryResult, props, depth+1)
			if err != nil {
				return err
			}
			continue
		}

		if depth == maxDepth {
			if aggDef.Type == dateHistType {
				err = p.processMetrics(esAgg, target, queryResult, props)
			} else {
				err = p.processAggregationDocs(esAgg, aggDef, target, queryResult, props)
			}
			if err != nil {
				return err
			}
		} else {
			for _, b := range esAgg.Get("buckets").MustArray() {
				bucket := simplejson.NewFromAny(b)
				newProps := make(map[string]string, len(props))

				for k, v := range props {
					newProps[k] = v
				}

				if key, ok := formatBucketKey(bucket); ok {
					newProps[aggDef.Field] = key
				}

				if key, err := bucket.Get("key_as_string").String(); err == nil {
					newProps[aggDef.Field] = key
				}
				// Dataplane long frames read the first outer date_histogram back as
				// a time column, so its key stays epoch millis regardless of format.
				if p.dataplane && aggDef.Type == dateHistType && !leafIsDateHistogram(target) && aggDef == firstDateHistogramAgg(target) {
					if key, ok := formatBucketKey(bucket); ok {
						newProps[aggDef.Field] = key
					}
				}
				err = p.processBuckets(bucket.MustMap(), target, queryResult, newProps, depth+1)
				if err != nil {
					return err
				}
			}

			buckets := esAgg.Get("buckets").MustMap()
			bucketKeys := make([]string, 0)
			for k := range buckets {
				bucketKeys = append(bucketKeys, k)
			}
			sort.Strings(bucketKeys)

			for _, bucketKey := range bucketKeys {
				bucket := simplejson.NewFromAny(buckets[bucketKey])
				newProps := make(map[string]string, len(props))

				for k, v := range props {
					newProps[k] = v
				}

				newProps[p.filterPropName(props, aggDef)] = bucketKey

				err = p.processBuckets(bucket.MustMap(), target, queryResult, newProps, depth+1)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// processMetrics processes metric aggregations from date histogram buckets
func (p *metricsResponseProcessor) processMetrics(esAgg *simplejson.Json, target *Query, query *backend.DataResponse,
	props map[string]string) error {
	frames := data.Frames{}
	esAggBuckets := esAgg.Get("buckets").MustArray()

	jsonBuckets := make([]*simplejson.Json, len(esAggBuckets))

	for i, v := range esAggBuckets {
		jsonBuckets[i] = simplejson.NewFromAny(v)
	}

	for _, metric := range target.Metrics {
		if metric.Hide {
			continue
		}

		switch metric.Type {
		case countType:
			countFrames, err := p.processCountMetric(metric, jsonBuckets, props)
			if err != nil {
				return fmt.Errorf("error processing count metric: %w", err)
			}
			frames = append(frames, countFrames...)
		case percentilesType:
			percentileFrames, err := p.processPercentilesMetric(metric, jsonBuckets, props)
			if err != nil {
				return fmt.Errorf("error processing percentiles metric: %w", err)
			}
			frames = append(frames, percentileFrames...)
		case topMetricsType:
			topMetricsFrames, err := p.processTopMetricsMetric(metric, jsonBuckets, props)
			if err != nil {
				return fmt.Errorf("error processing top metrics metric: %w", err)
			}
			frames = append(frames, topMetricsFrames...)
		case extendedStatsType:
			extendedStatsFrames, err := p.processExtendedStatsMetric(metric, jsonBuckets, props)
			if err != nil {
				return fmt.Errorf("error processing extended stats metric: %w", err)
			}

			frames = append(frames, extendedStatsFrames...)
		default:
			defaultFrames, err := p.processDefaultMetric(metric, jsonBuckets, props)
			if err != nil {
				return fmt.Errorf("error processing default metric: %w", err)
			}
			frames = append(frames, defaultFrames...)
		}
	}
	if query.Frames != nil {
		oldFrames := query.Frames
		frames = append(oldFrames, frames...)
	}
	query.Frames = frames
	return nil
}

// processCountMetric processes count metric aggregations
func (p *metricsResponseProcessor) processCountMetric(metric *MetricAgg, buckets []*simplejson.Json, props map[string]string) (data.Frames, error) {
	tags := make(map[string]string, len(props))
	timeVector := make([]time.Time, 0, len(buckets))
	values := make([]*float64, 0, len(buckets))

	for _, bucket := range buckets {
		value := castToFloat(bucket.Get("doc_count"))
		timeValue, err := getAsTime(bucket.Get("key"))
		if err != nil {
			return nil, err
		}
		timeVector = append(timeVector, timeValue)
		values = append(values, value)
	}

	for k, v := range props {
		tags[k] = v
	}
	tags["metric"] = countType
	p.tagMetricID(tags, metric)
	return data.Frames{newTimeSeriesFrame(timeVector, tags, values)}, nil
}

// processPercentilesMetric processes percentiles metric aggregations
func (p *metricsResponseProcessor) processPercentilesMetric(metric *MetricAgg, buckets []*simplejson.Json, props map[string]string) (data.Frames, error) {
	if len(buckets) == 0 {
		return data.Frames{}, nil
	}

	firstBucket := buckets[0]
	percentiles := firstBucket.GetPath(metric.ID, "values").MustMap()

	percentileKeys := make([]string, 0)
	for k := range percentiles {
		percentileKeys = append(percentileKeys, k)
	}
	sort.Strings(percentileKeys)

	frames := data.Frames{}

	for _, percentileName := range percentileKeys {
		tags := make(map[string]string, len(props))
		timeVector := make([]time.Time, 0, len(buckets))
		values := make([]*float64, 0, len(buckets))

		for k, v := range props {
			tags[k] = v
		}
		tags["metric"] = "p" + percentileName
		tags["field"] = metric.Field
		p.tagMetricID(tags, metric)
		for _, bucket := range buckets {
			value := castToFloat(bucket.GetPath(metric.ID, "values", percentileName))
			key := bucket.Get("key")
			timeValue, err := getAsTime(key)
			if err != nil {
				return nil, err
			}
			timeVector = append(timeVector, timeValue)
			values = append(values, value)
		}
		frames = append(frames, newTimeSeriesFrame(timeVector, tags, values))
	}

	return frames, nil
}

// processTopMetricsMetric processes top_metrics metric aggregations
func (p *metricsResponseProcessor) processTopMetricsMetric(metric *MetricAgg, buckets []*simplejson.Json, props map[string]string) (data.Frames, error) {
	metrics := metric.Settings.Get("metrics").MustArray()

	frames := data.Frames{}

	for _, metricField := range metrics {
		tags := make(map[string]string, len(props))
		timeVector := make([]time.Time, 0, len(buckets))
		values := make([]*float64, 0, len(buckets))
		for k, v := range props {
			tags[k] = v
		}

		tags["field"] = metricField.(string)
		tags["metric"] = "top_metrics"
		p.tagMetricID(tags, metric)

		for _, bucket := range buckets {
			stats := bucket.GetPath(metric.ID, "top")
			timeValue, err := getAsTime(bucket.Get("key"))
			if err != nil {
				return nil, err
			}
			timeVector = append(timeVector, timeValue)
			values = append(values, topMetricValue(stats.MustArray(), metricField.(string)))
		}

		frames = append(frames, newTimeSeriesFrame(timeVector, tags, values))
	}

	return frames, nil
}

// processExtendedStatsMetric processes extended_stats metric aggregations
func (p *metricsResponseProcessor) processExtendedStatsMetric(metric *MetricAgg, buckets []*simplejson.Json, props map[string]string) (data.Frames, error) {
	metaKeys := make([]string, 0)
	meta := metric.Meta.MustMap()
	for k := range meta {
		metaKeys = append(metaKeys, k)
	}
	sort.Strings(metaKeys)

	frames := data.Frames{}

	for _, statName := range metaKeys {
		v := meta[statName]
		if enabled, ok := v.(bool); !ok || !enabled {
			continue
		}

		tags := make(map[string]string, len(props))
		timeVector := make([]time.Time, 0, len(buckets))
		values := make([]*float64, 0, len(buckets))

		for k, v := range props {
			tags[k] = v
		}
		tags["metric"] = statName
		tags["field"] = metric.Field
		p.tagMetricID(tags, metric)

		for _, bucket := range buckets {
			timeValue, err := getAsTime(bucket.Get("key"))
			if err != nil {
				return nil, err
			}
			var value *float64
			switch statName {
			case "std_deviation_bounds_upper":
				value = castToFloat(bucket.GetPath(metric.ID, "std_deviation_bounds", "upper"))
			case "std_deviation_bounds_lower":
				value = castToFloat(bucket.GetPath(metric.ID, "std_deviation_bounds", "lower"))
			default:
				value = castToFloat(bucket.GetPath(metric.ID, statName))
			}
			timeVector = append(timeVector, timeValue)
			values = append(values, value)
		}
		labels := tags
		frames = append(frames, newTimeSeriesFrame(timeVector, labels, values))
	}

	return frames, nil
}

// processDefaultMetric processes default metric aggregations
func (p *metricsResponseProcessor) processDefaultMetric(metric *MetricAgg, buckets []*simplejson.Json, props map[string]string) (data.Frames, error) {
	tags := make(map[string]string, len(props))
	timeVector := make([]time.Time, 0, len(buckets))
	values := make([]*float64, 0, len(buckets))

	for k, v := range props {
		tags[k] = v
	}

	tags["metric"] = metric.Type
	tags["field"] = metric.Field
	tags["metricId"] = metric.ID
	for _, bucket := range buckets {
		timeValue, err := getAsTime(bucket.Get("key"))
		if err != nil {
			return nil, err
		}
		valueObj, err := bucket.Get(metric.ID).Map()
		if err != nil {
			continue
		}
		var value *float64
		if _, ok := valueObj["normalized_value"]; ok {
			value = castToFloat(bucket.GetPath(metric.ID, "normalized_value"))
		} else {
			value = castToFloat(bucket.GetPath(metric.ID, "value"))
		}
		timeVector = append(timeVector, timeValue)
		values = append(values, value)
	}
	return data.Frames{newTimeSeriesFrame(timeVector, tags, values)}, nil
}

// ensurePropFields guarantees all property columns exist even if prior frames lacked them
func ensurePropFields(fields *[]*data.Field, keys []string) {
	have := map[string]bool{}
	for _, f := range *fields {
		have[f.Name] = true
	}
	for _, k := range keys {
		if !have[k] {
			d := ""
			f := extractDataField(k, &d)
			*fields = append(*fields, f)
		}
	}
}

// appendPropsRow appends one row of property values; skipKey avoids double-append
func appendPropsRow(fields *[]*data.Field, props map[string]string, propKeys []string, skipKey string) {
	for _, f := range *fields {
		for _, pk := range propKeys {
			if pk == skipKey {
				continue
			}
			if f.Name == pk {
				val := props[pk]
				f.Append(&val)
			}
		}
	}
}

// appendMetrics appends all metric values for a single bucket/row; a repeated column name gets the metric id as suffix.
func appendMetrics(fields *[]*data.Field, bucket *simplejson.Json, target *Query) {
	used := map[string]struct{}{}
	for _, metric := range target.Metrics {
		switch metric.Type {
		case countType:
			addMetricValueToFields(fields, used, getMetricName(metric.Type), metric.ID, castToFloat(bucket.Get("doc_count")))
		case extendedStatsType:
			addExtendedStatsToFields(fields, used, bucket, metric)
		case percentilesType:
			addPercentilesToFields(fields, used, bucket, metric)
		case topMetricsType:
			addTopMetricsToFields(fields, used, bucket, metric)
		default:
			addOtherMetricsToFields(fields, used, bucket, metric, target)
		}
	}
}

// appendKeyColumnString appends a string key to an existing field or creates it
func appendKeyColumnString(fields *[]*data.Field, fieldName, key string) {
	for _, f := range *fields {
		if f.Name == fieldName {
			k := key
			f.Append(&k)
			return
		}
	}
	k := key
	f := extractDataField(fieldName, &k)
	f.Append(&k)
	*fields = append(*fields, f)
}

// appendBucketKeyValue appends the bucket's "key" to fieldName; stringKeys forces a
// string column so dataplane long frames read it as a dimension.
func appendBucketKeyValue(fields *[]*data.Field, fieldName string, bucket *simplejson.Json, stringKeys bool) error {
	if stringKeys {
		key, err := bucket.Get("key_as_string").String()
		if err != nil {
			var ok bool
			if key, ok = formatBucketKey(bucket); !ok {
				return fmt.Errorf("bucket key for field %q is neither string nor number", fieldName)
			}
		}
		appendKeyColumnString(fields, fieldName, key)
		return nil
	}
	for _, f := range *fields {
		if f.Name == fieldName {
			if s, err := bucket.Get("key").String(); err == nil {
				f.Append(&s)
				return nil
			}
			num, err := bucket.Get("key").Float64()
			if err != nil {
				return fmt.Errorf("error appending bucket key to existing field %q: %w", fieldName, err)
			}
			f.Append(&num)
			return nil
		}
	}

	// field not present yet
	if s, err := bucket.Get("key").String(); err == nil {
		f := extractDataField(fieldName, &s)
		f.Append(&s)
		*fields = append(*fields, f)
		return nil
	}

	num, err := bucket.Get("key").Float64()
	if err != nil {
		return fmt.Errorf("error appending bucket key to new field %q: %w", fieldName, err)
	}

	f := extractDataField(fieldName, &num)
	f.Append(&num)
	*fields = append(*fields, f)

	return nil
}

func (p *metricsResponseProcessor) processAggregationDocs(
	esAgg *simplejson.Json,
	aggDef *BucketAgg,
	target *Query,
	queryResult *backend.DataResponse,
	props map[string]string,
) error {
	propKeys := createPropKeys(props)
	buckets := esAgg.Get("buckets")

	if arr := buckets.MustArray(); len(arr) > 0 {
		fields := createFields(queryResult.Frames, propKeys)
		ensurePropFields(&fields, propKeys)

		for _, v := range arr {
			bucket := simplejson.NewFromAny(v)

			appendPropsRow(&fields, props, propKeys, "")
			keyFieldName := aggDef.Field
			if keyFieldName == "" && p.dataplane {
				keyFieldName = "key"
			}
			if keyFieldName != "" {
				if err := appendBucketKeyValue(&fields, keyFieldName, bucket, p.dataplane); err != nil {
					return err
				}
			}
			appendMetrics(&fields, bucket, target)
		}

		queryResult.Frames = data.Frames{&data.Frame{Fields: fields}}
		return nil
	}

	if m := buckets.MustMap(); len(m) > 0 {
		filterKey := p.filterPropName(props, aggDef)
		keyFieldName := aggDef.Field
		if keyFieldName == "" {
			keyFieldName = filterKey
		}

		hasFilter := false
		for _, pk := range propKeys {
			if pk == filterKey {
				hasFilter = true
				break
			}
		}
		if !hasFilter {
			propKeys = append(propKeys, filterKey)
		}

		fields := createFields(queryResult.Frames, propKeys)
		ensurePropFields(&fields, propKeys)

		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			bucket := simplejson.NewFromAny(m[k])

			locProps := make(map[string]string, len(props)+1)
			for kk, vv := range props {
				locProps[kk] = vv
			}
			locProps[filterKey] = k

			// avoid double-append when the key column is the filter dimension
			skip := ""
			if keyFieldName == filterKey {
				skip = filterKey
			}

			appendPropsRow(&fields, locProps, propKeys, skip)
			appendKeyColumnString(&fields, keyFieldName, k)
			appendMetrics(&fields, bucket, target)
		}

		queryResult.Frames = data.Frames{&data.Frame{Fields: fields}}
		return nil
	}

	// no buckets present: keep rows accumulated from earlier outer buckets
	if queryResult.Frames == nil {
		queryResult.Frames = data.Frames{}
	}
	return nil
}

// newTimeSeriesFrame creates a new time series frame
func newTimeSeriesFrame(timeData []time.Time, tags map[string]string, values []*float64) *data.Frame {
	frame := data.NewFrame("",
		data.NewField(data.TimeSeriesTimeFieldName, nil, timeData),
		data.NewField(data.TimeSeriesValueFieldName, tags, values))
	frame.Meta = &data.FrameMeta{
		Type: data.FrameTypeTimeSeriesMulti,
	}
	return frame
}

// trimDatapoints trims datapoints from the beginning and end of the results
func trimDatapoints(queryResult backend.DataResponse, target *Query) {
	var histogram *BucketAgg
	for _, bucketAgg := range target.BucketAggs {
		if bucketAgg.Type == dateHistType {
			histogram = bucketAgg
			break
		}
	}

	if histogram == nil {
		return
	}

	trimEdges, err := castToInt(histogram.Settings.Get("trimEdges"))
	if err != nil {
		return
	}

	frames := queryResult.Frames

	for _, frame := range frames {
		for _, field := range frame.Fields {
			if field.Len() > trimEdges*2 {
				// first we delete the first "trim" items
				for i := 0; i < trimEdges; i++ {
					field.Delete(0)
				}

				// then we delete the last "trim" items
				for i := 0; i < trimEdges; i++ {
					field.Delete(field.Len() - 1)
				}
			}
		}
	}
}

// Helper functions for adding metrics to fields

func addMetricValueToFields(fields *[]*data.Field, used map[string]struct{}, metricName, metricID string, value *float64) {
	if _, taken := used[metricName]; taken {
		metricName += " " + metricID
	}
	used[metricName] = struct{}{}
	index := -1
	for i, f := range *fields {
		if f.Name == metricName {
			index = i
			break
		}
	}

	var field data.Field
	if index == -1 {
		field = *data.NewField(metricName, nil, []*float64{})
		*fields = append(*fields, &field)
	} else {
		field = *(*fields)[index]
	}
	field.Append(value)
}

func addPercentilesToFields(fields *[]*data.Field, used map[string]struct{}, bucket *simplejson.Json, metric *MetricAgg) {
	percentiles := bucket.GetPath(metric.ID, "values")
	for _, percentileName := range getSortedKeys(percentiles.MustMap()) {
		percentileValue := percentiles.Get(percentileName).MustFloat64()
		addMetricValueToFields(fields, used, fmt.Sprintf("p%v %v", percentileName, metric.Field), metric.ID, &percentileValue)
	}
}

func addExtendedStatsToFields(fields *[]*data.Field, used map[string]struct{}, bucket *simplejson.Json, metric *MetricAgg) {
	metaKeys := make([]string, 0)
	meta := metric.Meta.MustMap()
	for k := range meta {
		metaKeys = append(metaKeys, k)
	}
	sort.Strings(metaKeys)
	for _, statName := range metaKeys {
		v := meta[statName]
		if enabled, ok := v.(bool); !ok || !enabled {
			continue
		}
		var value *float64
		switch statName {
		case "std_deviation_bounds_upper":
			value = castToFloat(bucket.GetPath(metric.ID, "std_deviation_bounds", "upper"))
		case "std_deviation_bounds_lower":
			value = castToFloat(bucket.GetPath(metric.ID, "std_deviation_bounds", "lower"))
		default:
			value = castToFloat(bucket.GetPath(metric.ID, statName))
		}

		addMetricValueToFields(fields, used, getMetricName(metric.Type), metric.ID, value)
		break
	}
}

func addTopMetricsToFields(fields *[]*data.Field, used map[string]struct{}, bucket *simplejson.Json, metric *MetricAgg) {
	baseName := getMetricName(metric.Type)
	metrics := metric.Settings.Get("metrics").MustStringArray()
	for _, metricField := range metrics {
		// If we selected more than one metric we also add each metric name
		metricName := baseName
		if len(metrics) > 1 {
			metricName += " " + metricField
		}
		top := bucket.GetPath(metric.ID, "top").MustArray()
		addMetricValueToFields(fields, used, metricName, metric.ID, topMetricValue(top, metricField))
	}
}

func addOtherMetricsToFields(fields *[]*data.Field, used map[string]struct{}, bucket *simplejson.Json, metric *MetricAgg, target *Query) {
	metricName := getMetricName(metric.Type)
	otherMetrics := make([]*MetricAgg, 0)

	for _, m := range target.Metrics {
		// To other metrics we add metric of the same type that are not the current metric
		if m.ID != metric.ID && m.Type == metric.Type {
			otherMetrics = append(otherMetrics, m)
		}
	}

	if len(otherMetrics) > 0 {
		metricName += " " + metric.Field

		// We check if we have metric with the same type and same field name
		// If so, append metric.ID to the metric name
		for _, m := range otherMetrics {
			if m.Field == metric.Field {
				metricName += " " + metric.ID
				break
			}
		}

		if metric.Type == "bucket_script" {
			// Use the formula in the column name
			metricName = metric.Settings.Get("script").MustString("")
		}
	}
	addMetricValueToFields(fields, used, metricName, metric.ID, castToFloat(bucket.GetPath(metric.ID, "value")))
}

func extractDataField(name string, v interface{}) *data.Field {
	var field *data.Field
	switch v.(type) {
	case *string:
		field = data.NewField(name, nil, []*string{})
	case *float64:
		field = data.NewField(name, nil, []*float64{})
	default:
		field = &data.Field{}
	}
	isFilterable := true
	field.Config = &data.FieldConfig{Filterable: &isFilterable}
	return field
}

// formatBucketKey returns the bucket's "key" as a string; integral numbers keep
// their int64 rendering and fractions are not truncated.
func formatBucketKey(bucket *simplejson.Json) (string, bool) {
	key := bucket.Get("key")
	if s, err := key.String(); err == nil {
		return s, true
	}
	f, err := key.Float64()
	if err != nil {
		return "", false
	}
	switch {
	case f != math.Trunc(f):
		return strconv.FormatFloat(f, 'f', -1, 64), true
	case math.Abs(f) < 1<<63:
		return strconv.FormatInt(int64(f), 10), true
	default:
		return strconv.FormatFloat(f, 'f', 0, 64), true
	}
}

// filterPropName names a filters level's dimension; under the dataplane flag a
// filters level nested in another one gets its agg id so the two stay distinct.
func (p *metricsResponseProcessor) filterPropName(props map[string]string, aggDef *BucketAgg) string {
	if _, taken := props["filter"]; taken && p.dataplane {
		return "filter " + aggDef.ID
	}
	return "filter"
}

// topMetricValue returns the first top hit's value for field, or nil when no
// hit carries one, so every bucket contributes exactly one point.
func topMetricValue(hits []interface{}, field string) *float64 {
	for _, hit := range hits {
		stat, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}
		metrics, ok := stat["metrics"].(map[string]interface{})
		if !ok {
			continue
		}
		if v, ok := metrics[field].(float64); ok {
			return &v
		}
	}
	return nil
}

// tagMetricID records the metric id as an internal label so dataplane naming
// can tell apart items whose generated names collide; nameFields strips it.
func (p *metricsResponseProcessor) tagMetricID(tags map[string]string, metric *MetricAgg) {
	if p.dataplane {
		tags["metricId"] = metric.ID
	}
}
