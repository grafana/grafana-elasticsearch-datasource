package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFindTheFirstNonNilDocValueForPropName(t *testing.T) {
	t.Run("returns nil for empty docs without panicking", func(t *testing.T) {
		require.NotPanics(t, func() {
			require.Nil(t, findTheFirstNonNilDocValueForPropName(nil, "field"))
			require.Nil(t, findTheFirstNonNilDocValueForPropName([]map[string]interface{}{}, "field"))
		})
	})

	t.Run("returns the first non-nil value", func(t *testing.T) {
		docs := []map[string]interface{}{
			{"field": nil},
			{"field": "value"},
		}
		require.Equal(t, "value", findTheFirstNonNilDocValueForPropName(docs, "field"))
	})

	t.Run("falls back to the first doc's value when all are nil", func(t *testing.T) {
		docs := []map[string]interface{}{{"field": nil}}
		require.Nil(t, findTheFirstNonNilDocValueForPropName(docs, "field"))
	})
}
