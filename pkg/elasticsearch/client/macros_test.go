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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := interpolateSearchBody(tt.body, tt.interval)
			require.NoError(t, err)
			require.Equal(t, tt.expected, result)
		})
	}
}
