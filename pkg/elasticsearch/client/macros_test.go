package client

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInterpolateSearchBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		interval time.Duration
		expected string
	}{
		{
			name:     "no macros returns body unchanged",
			body:     `{"size":0}`,
			interval: 15 * time.Second,
			expected: `{"size":0}`,
		},
		{
			name:     "interval expands to duration string",
			body:     `{"fixed_interval":"$__interval"}`,
			interval: 15 * time.Second,
			expected: `{"fixed_interval":"15s"}`,
		},
		{
			name:     "interval_ms expands to milliseconds",
			body:     `{"script":"$__interval_ms*@hostname"}`,
			interval: 15 * time.Second,
			expected: `{"script":"15000*@hostname"}`,
		},
		{
			name:     "interval_msms expands to milliseconds with ms unit",
			body:     `{"fixed_interval":"$__interval_msms"}`,
			interval: 500 * time.Millisecond,
			expected: `{"fixed_interval":"500ms"}`,
		},
		{
			name:     "all interval macros expand in one body",
			body:     `{"a":"$__interval","b":"$__interval_ms","c":"$__interval_msms"}`,
			interval: time.Minute,
			expected: `{"a":"1m0s","b":"60000","c":"60000ms"}`,
		},
		{
			name:     "interval_ms falls back to 1000 for zero interval",
			body:     `{"script":"$__interval_ms"}`,
			interval: 0,
			expected: `{"script":"1000"}`,
		},
		{
			name:     "unknown macros are left untouched",
			body:     `{"query":"$__timeFilter(@timestamp)"}`,
			interval: 15 * time.Second,
			expected: `{"query":"$__timeFilter(@timestamp)"}`,
		},
		{
			name:     "macro names are not matched as substrings of longer names",
			body:     `{"query":"$__intervalfoo"}`,
			interval: 15 * time.Second,
			expected: `{"query":"$__intervalfoo"}`,
		},
		{
			name:     "repeated macros expand at every occurrence",
			body:     `{"a":"$__interval_ms","b":"$__interval_ms"}`,
			interval: 15 * time.Second,
			expected: `{"a":"15000","b":"15000"}`,
		},
		{
			name:     "macros expand inside JSON strings containing escaped quotes",
			body:     `{"query":"say \"hi\" then wait $__interval"}`,
			interval: 15 * time.Second,
			expected: `{"query":"say \"hi\" then wait 15s"}`,
		},
		{
			name:     "macros expand next to raw DSL comment markers",
			body:     `{"script":"a--$__interval_ms"}`,
			interval: 15 * time.Second,
			expected: `{"script":"a--15000"}`,
		},
		{
			name:     "sub-second interval keeps millisecond duration formatting",
			body:     `{"fixed_interval":"$__interval"}`,
			interval: 500 * time.Millisecond,
			expected: `{"fixed_interval":"500ms"}`,
		},
		{
			name:     "macro names are case sensitive",
			body:     `{"script":"$__INTERVAL_MS"}`,
			interval: 15 * time.Second,
			expected: `{"script":"$__INTERVAL_MS"}`,
		},
		{
			name:     "bare prefix without a name is left untouched",
			body:     `{"script":"$__ $__."}`,
			interval: 15 * time.Second,
			expected: `{"script":"$__ $__."}`,
		},
		{
			name:     "parentheses after a known macro are consumed as arguments",
			body:     `{"script":"$__interval_ms(x)"}`,
			interval: 15 * time.Second,
			expected: `{"script":"15000"}`,
		},
		{
			name:     "unbalanced parenthesis after a known macro expands as zero-arg",
			body:     `{"script":"$__interval_ms("}`,
			interval: 15 * time.Second,
			expected: `{"script":"15000("}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interpolateSearchBody(tt.body, tt.interval)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}
