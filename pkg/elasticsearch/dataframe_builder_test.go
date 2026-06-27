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

func TestCreatePropKeys(t *testing.T) {
	t.Run("returns keys sorted alphabetically", func(t *testing.T) {
		props := map[string]string{"zeta": "1", "alpha": "2", "mike": "3"}
		require.Equal(t, []string{"alpha", "mike", "zeta"}, createPropKeys(props))
	})

	t.Run("returns an empty slice for an empty map", func(t *testing.T) {
		require.Empty(t, createPropKeys(map[string]string{}))
	})
}
