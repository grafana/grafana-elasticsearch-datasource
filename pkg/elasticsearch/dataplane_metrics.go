package elasticsearch

import (
	"sort"
	"strconv"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Dataplane-compliant metrics frames are gated behind the featureflags.MetricsDataplane
// GOFF flag; contract: https://github.com/grafana/dataplane/blob/main/docs/contract/ (timeseries, numeric).

// dataplaneMetricsVersion is the contract version of every metrics kind emitted.
var dataplaneMetricsVersion = data.FrameTypeVersion{0, 1}

func setMetricsFrameMeta(frame *data.Frame, frameType data.FrameType) {
	if frame.Meta == nil {
		frame.Meta = &data.FrameMeta{}
	}
	frame.Meta.Type = frameType
	frame.Meta.TypeVersion = dataplaneMetricsVersion
}

// nameDataplaneSeries names the value field after the metric item; the metric id is
// appended when the name and labels collide with a different metric's series.
func nameDataplaneSeries(frame *data.Frame, valueField *data.Field, target *Query, metricTypeCount int, seen map[string]string) {
	metricID := valueField.Labels["metricId"]
	itemName := metricItemName(valueField.Labels, target)
	displayName := getFieldName(*valueField, target, metricTypeCount)
	delete(valueField.Labels, "metricId")
	if len(valueField.Labels) == 0 {
		valueField.Labels = nil
	}

	key := itemName + "\x00" + valueField.Labels.String()
	if owner, dup := seen[key]; dup && owner != metricID {
		itemName += " " + metricID
		key = itemName + "\x00" + valueField.Labels.String()
	}
	if _, dup := seen[key]; !dup {
		seen[key] = metricID
	}

	valueField.Name = itemName
	if valueField.Config == nil {
		valueField.Config = &data.FieldConfig{}
	}
	valueField.Config.DisplayNameFromDS = displayName
	frame.Name = displayName
	setMetricsFrameMeta(frame, data.FrameTypeTimeSeriesMulti)
}

// finalizeDataplaneMetricsFrames types the parsed frames and replaces an empty
// result with the contract's single typed frame.
func finalizeDataplaneMetricsFrames(queryRes *backend.DataResponse, target *Query) {
	switch {
	case leafIsDateHistogram(target):
		for _, frame := range queryRes.Frames {
			sortRowsByTime(frame, 0)
		}
		trimDatapoints(*queryRes, target)
	case len(queryRes.Frames) == 1:
		finalizeDataplaneTableFrame(queryRes.Frames[0], target)
	}
	if len(queryRes.Frames) == 0 {
		queryRes.Frames = data.Frames{emptyMetricsFrame(target)}
	}
}

// finalizeDataplaneTableFrame types a table frame as timeseries-long (outer
// date_histogram) or numeric-long; a key column that does not parse as epoch
// millis, or a malformed frame, is left untyped.
func finalizeDataplaneTableFrame(frame *data.Frame, target *Query) {
	if _, err := frame.RowLen(); err != nil {
		return
	}
	dateAgg := firstDateHistogramAgg(target)
	if dateAgg == nil {
		setMetricsFrameMeta(frame, data.FrameTypeNumericLong)
		return
	}
	timeIdx := fieldIndexByName(frame, dateAgg.Field)
	if timeIdx < 0 {
		return
	}
	times, ok := epochMillisColumnToTimes(frame.Fields[timeIdx])
	if !ok {
		return
	}
	timeField := data.NewField(dateAgg.Field, nil, times)
	timeField.Config = frame.Fields[timeIdx].Config
	frame.Fields[timeIdx] = timeField
	sortRowsByTime(frame, timeIdx)
	if trimEdges, err := castToInt(dateAgg.Settings.Get("trimEdges")); err == nil && trimEdges > 0 {
		trimRowsAtTimeEdges(frame, timeIdx, trimEdges)
	}
	setMetricsFrameMeta(frame, data.FrameTypeTimeSeriesLong)
}

// emptyMetricsFrame is the contract's "No Data" response for the kind the
// query would otherwise have produced.
func emptyMetricsFrame(target *Query) *data.Frame {
	frameType := data.FrameTypeNumericLong
	switch {
	case leafIsDateHistogram(target):
		frameType = data.FrameTypeTimeSeriesMulti
	case firstDateHistogramAgg(target) != nil:
		frameType = data.FrameTypeTimeSeriesLong
	}
	return newEmptyMetricsFrame(target.RefID, frameType)
}

func newEmptyMetricsFrame(refID string, frameType data.FrameType) *data.Frame {
	frame := data.NewFrame("")
	frame.RefID = refID
	setMetricsFrameMeta(frame, frameType)
	return frame
}

func leafIsDateHistogram(target *Query) bool {
	n := len(target.BucketAggs)
	return n > 0 && target.BucketAggs[n-1].Type == dateHistType
}

func firstDateHistogramAgg(target *Query) *BucketAgg {
	for _, agg := range target.BucketAggs {
		if agg.Type == dateHistType {
			return agg
		}
	}
	return nil
}

func fieldIndexByName(frame *data.Frame, name string) int {
	for i, f := range frame.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// epochMillisColumnToTimes parses a *string column of epoch-millisecond keys.
func epochMillisColumnToTimes(field *data.Field) ([]time.Time, bool) {
	times := make([]time.Time, field.Len())
	for i := range times {
		key, ok := field.At(i).(*string)
		if !ok || key == nil {
			return nil, false
		}
		ms, err := strconv.ParseInt(*key, 10, 64)
		if err != nil {
			return nil, false
		}
		times[i] = time.UnixMilli(ms).UTC()
	}
	return times, true
}

// sortRowsByTime orders the frame's rows by its []time.Time column at timeIdx,
// keeping the response order of rows that share a timestamp.
func sortRowsByTime(frame *data.Frame, timeIdx int) {
	n, err := frame.RowLen()
	if err != nil || n < 2 || frame.Fields[timeIdx].Type() != data.FieldTypeTime {
		return
	}
	times := frame.Fields[timeIdx]
	before := func(a, b int) bool {
		return times.At(a).(time.Time).Before(times.At(b).(time.Time))
	}
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	byTime := func(a, b int) bool { return before(perm[a], perm[b]) }
	if sort.SliceIsSorted(perm, byTime) {
		return
	}
	sort.SliceStable(perm, byTime)
	for i, f := range frame.Fields {
		sorted := data.NewFieldFromFieldType(f.Type(), n)
		sorted.Name, sorted.Labels, sorted.Config = f.Name, f.Labels, f.Config
		for row, src := range perm {
			sorted.Set(row, f.At(src))
		}
		frame.Fields[i] = sorted
	}
}

// trimRowsAtTimeEdges drops every row that falls in the first and last trim
// distinct timestamps, the long-frame equivalent of trimDatapoints.
func trimRowsAtTimeEdges(frame *data.Frame, timeIdx int, trim int) {
	times := frame.Fields[timeIdx]
	distinct := make([]time.Time, 0)
	for i := 0; i < times.Len(); i++ {
		t := times.At(i).(time.Time)
		if len(distinct) == 0 || !distinct[len(distinct)-1].Equal(t) {
			distinct = append(distinct, t)
		}
	}
	if len(distinct) <= trim*2 {
		return
	}
	cut := make(map[time.Time]struct{}, trim*2)
	for _, t := range distinct[:trim] {
		cut[t] = struct{}{}
	}
	for _, t := range distinct[len(distinct)-trim:] {
		cut[t] = struct{}{}
	}
	for i := times.Len() - 1; i >= 0; i-- {
		if _, drop := cut[times.At(i).(time.Time)]; drop {
			frame.DeleteRow(i)
		}
	}
}
