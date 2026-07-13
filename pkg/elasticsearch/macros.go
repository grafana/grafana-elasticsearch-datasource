package elasticsearch

import (
	"github.com/grafana/macropro"
)

// esqlMacros are the macros supported in ES|QL queries. The QueryContext
// Extra field carries the resolved index pattern for $__index.
var esqlMacros = macropro.MacroMap[string]{
	"index": func(ctx macropro.QueryContext[string], _ []string) (string, error) {
		return ctx.Extra, nil
	},
}

// interpolateEsqlQuery expands macros in an ES|QL query. Comment stripping is
// disabled to keep the query text intact: ES|QL uses // and /* */ comments
// rather than SQL's --, and Elasticsearch handles them itself.
func interpolateEsqlQuery(query string, index string) (string, error) {
	return macropro.Interpolate(query, esqlMacros, macropro.QueryContext[string]{
		Extra: index,
	}, macropro.WithComments(0))
}
