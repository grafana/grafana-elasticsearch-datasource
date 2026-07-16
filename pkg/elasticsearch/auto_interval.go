package elasticsearch

import (
	"time"
)

// maxTotalBuckets mirrors the Elasticsearch search.max_buckets default (65,535 in
// 7.x, 65,536 in 8.x and later). The lower value keeps the estimate safe on both.
const maxTotalBuckets = int64(65535)

// niceIntervals is the ladder of fixed intervals a clamped auto interval is rounded
// up to, so date histogram buckets keep human-readable boundaries. Values above one
// day are rounded to whole days instead.
var niceIntervals = []time.Duration{
	time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
	20 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond,
	200 * time.Millisecond, 500 * time.Millisecond,
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// clampAutoInterval widens the panel interval when an auto-interval date histogram is
// combined with terms or filters aggregations. The panel interval only accounts for
// time range / max data points, but Elasticsearch materialises every time bucket in
// range for every parent bucket (the builder always sets min_doc_count 0 with extended
// bounds), so total buckets are time buckets multiplied by the parent bucket count.
// Left unclamped, wide ranges fail with "Trying to create too many buckets"
// (https://github.com/grafana/grafana-elasticsearch-datasource/issues/383).
// Elasticsearch's auto_date_histogram back-calculates its interval from the bucket
// limit the same way.
func clampAutoInterval(q *Query, fromMs, toMs int64) time.Duration {
	interval := q.Interval
	if !hasAutoDateHistogram(q.BucketAggs) {
		return interval
	}
	multiplier := parentBucketMultiplier(q.BucketAggs)
	if multiplier <= 1 {
		return interval
	}
	rangeMs := toMs - fromMs
	if rangeMs <= 0 {
		return interval
	}
	// budget is the number of time buckets allowed per parent bucket, keeping one
	// multiplier of headroom per aggregation level for the parent buckets themselves.
	budget := maxTotalBuckets/multiplier - int64(len(q.BucketAggs))
	// A date histogram over the range produces up to rangeMs/interval + 2 buckets
	// (extended bounds plus boundary rounding), so a budget below 3 cannot be met by
	// any interval: leave the query alone and let Elasticsearch report the overflow.
	if budget <= 2 {
		return interval
	}
	effective := interval
	if effective <= 0 {
		// Matches the fallback applied when the request is encoded.
		effective = time.Second
	}
	if rangeMs/effective.Milliseconds()+2 <= budget {
		return interval
	}
	floorMs := (rangeMs + budget - 3) / (budget - 2) // ceil(rangeMs / (budget - 2))
	return roundUpInterval(floorMs)
}

// hasAutoDateHistogram reports whether the query contains a date histogram whose
// interval is resolved from the panel interval. Explicit fixed and calendar intervals
// are the user's own choice and are never clamped.
func hasAutoDateHistogram(bucketAggs []*BucketAgg) bool {
	for _, agg := range bucketAggs {
		if agg.Type == dateHistType && agg.Settings.Get("interval").MustString("auto") == "auto" {
			return true
		}
	}
	return false
}

// parentBucketMultiplier returns the worst-case number of buckets the non-histogram
// bucket aggregations multiply the date histogram by. Terms cardinality is capped by
// the requested size and filters create one bucket per filter. Histogram and geohash
// grid cardinality cannot be known up front, so they do not contribute.
func parentBucketMultiplier(bucketAggs []*BucketAgg) int64 {
	multiplier := int64(1)
	for _, agg := range bucketAggs {
		switch agg.Type {
		case termsType:
			size := int64(termsAggSize(agg))
			if size <= 0 {
				size = defaultSize
			}
			multiplier *= size
		case filtersType:
			if n := len(agg.Settings.Get("filters").MustArray()); n > 0 {
				multiplier *= int64(n)
			}
		}
		if multiplier > maxTotalBuckets {
			// Already over the limit, so no interval can help; stopping here also
			// guards against overflow.
			return multiplier
		}
	}
	return multiplier
}

// roundUpInterval rounds intervalMs up to the nearest nice fixed interval, falling
// back to whole days beyond the ladder. Rounding up only ever produces fewer buckets,
// so the clamp stays safe.
func roundUpInterval(intervalMs int64) time.Duration {
	interval := time.Duration(intervalMs) * time.Millisecond
	for _, nice := range niceIntervals {
		if nice >= interval {
			return nice
		}
	}
	day := 24 * time.Hour
	days := int64((interval + day - 1) / day)
	return time.Duration(days) * day
}
