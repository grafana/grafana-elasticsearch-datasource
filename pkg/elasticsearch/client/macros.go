package client

import (
	"strconv"
	"time"

	"github.com/grafana/macropro"
)

// searchBodyMacros are the macros supported in encoded search request bodies,
// covering both backend-built aggregations and user-authored raw DSL queries.
//
// interval_msms is not user-facing: addDateHistogramAgg emits the
// $__interval_msms placeholder for "auto" date histogram intervals because
// fixed_interval needs a single-unit value, and the always-milliseconds form
// (e.g. "500ms") is valid for any interval, unlike $__interval whose
// time.Duration formatting can produce multi-unit values such as "1m30s".
var searchBodyMacros = macropro.MacroMap[struct{}]{
	"interval": func(ctx macropro.QueryContext[struct{}], _ []string) (string, error) {
		return ctx.Interval.String(), nil
	},
	"interval_ms": func(ctx macropro.QueryContext[struct{}], _ []string) (string, error) {
		return strconv.FormatInt(ctx.IntervalMS, 10), nil
	},
	"interval_msms": func(ctx macropro.QueryContext[struct{}], _ []string) (string, error) {
		return strconv.FormatInt(ctx.IntervalMS, 10) + "ms", nil
	},
}

// interpolateSearchBody expands interval macros in an encoded search request
// body. Comment stripping is disabled because the body is JSON rather than
// SQL, and macros must expand wherever they appear, including inside JSON
// string values.
func interpolateSearchBody(body string, interval time.Duration) (string, error) {
	intervalMS := interval.Milliseconds()
	if intervalMS <= 0 {
		intervalMS = 1000
	}
	return macropro.Interpolate(body, searchBodyMacros, macropro.QueryContext[struct{}]{
		Interval:   interval,
		IntervalMS: intervalMS,
	}, macropro.WithComments(0))
}
