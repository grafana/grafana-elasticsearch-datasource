package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInterpolateEsqlQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		index    string
		expected string
	}{
		{
			name:     "index macro expands to index pattern",
			query:    "FROM $__index | LIMIT 10",
			index:    "logs-*",
			expected: "FROM logs-* | LIMIT 10",
		},
		{
			name:     "index macro expands at every occurrence",
			query:    "FROM $__index | LOOKUP JOIN $__index ON id",
			index:    "logs-*",
			expected: "FROM logs-* | LOOKUP JOIN logs-* ON id",
		},
		{
			name:     "no macros returns query unchanged",
			query:    "FROM logs-* | LIMIT 10",
			index:    "logs-*",
			expected: "FROM logs-* | LIMIT 10",
		},
		{
			name:     "macro names are not matched as substrings of longer names",
			query:    "FROM $__indexes | LIMIT 10",
			index:    "logs-*",
			expected: "FROM $__indexes | LIMIT 10",
		},
		{
			name:     "unknown macros are left untouched",
			query:    "FROM $__index | WHERE ts > $__timeFrom",
			index:    "logs-*",
			expected: "FROM logs-* | WHERE ts > $__timeFrom",
		},
		{
			name:     "index pattern lists expand verbatim",
			query:    "FROM $__index | LIMIT 10",
			index:    "logs-*,metrics-*",
			expected: "FROM logs-*,metrics-* | LIMIT 10",
		},
		{
			name:     "macro names are case sensitive",
			query:    "FROM $__INDEX | LIMIT 10",
			index:    "logs-*",
			expected: "FROM $__INDEX | LIMIT 10",
		},
		{
			name:     "macros inside ES|QL comments still expand",
			query:    "FROM $__index // $__index\n| LIMIT 10",
			index:    "logs-*",
			expected: "FROM logs-* // logs-*\n| LIMIT 10",
		},
		{
			name:     "macros inside ES|QL string literals still expand",
			query:    `FROM $__index | WHERE msg == "$__index"`,
			index:    "logs-*",
			expected: `FROM logs-* | WHERE msg == "logs-*"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interpolateEsqlQuery(tt.query, tt.index)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}
