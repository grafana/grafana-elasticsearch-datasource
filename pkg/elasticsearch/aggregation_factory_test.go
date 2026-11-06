package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsCalendarInterval(t *testing.T) {
	t.Run("recognises calendar intervals", func(t *testing.T) {
		for _, interval := range []string{"1w", "1M", "1q", "1y"} {
			require.True(t, isCalendarInterval(interval), "%q should be a calendar interval", interval)
		}
	})

	t.Run("rejects fixed and unknown intervals", func(t *testing.T) {
		for _, interval := range []string{"1d", "5m", "1h", "", "2w", "1W"} {
			require.False(t, isCalendarInterval(interval), "%q should not be a calendar interval", interval)
		}
	})
}
