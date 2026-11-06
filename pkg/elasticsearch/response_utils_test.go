package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

func TestUnwrapFieldValue(t *testing.T) {
	t.Run("Should unwrap single-element array to scalar", func(t *testing.T) {
		result := unwrapFieldValue([]interface{}{"hello"})
		require.Equal(t, "hello", result)
	})

	t.Run("Should unwrap single-element numeric array", func(t *testing.T) {
		result := unwrapFieldValue([]interface{}{42.0})
		require.Equal(t, 42.0, result)
	})

	t.Run("Should not unwrap multi-element array", func(t *testing.T) {
		input := []interface{}{"a", "b"}
		result := unwrapFieldValue(input)
		require.Equal(t, input, result)
	})

	t.Run("Should not unwrap empty array", func(t *testing.T) {
		input := []interface{}{}
		result := unwrapFieldValue(input)
		require.Equal(t, input, result)
	})

	t.Run("Should return scalar values unchanged", func(t *testing.T) {
		require.Equal(t, "hello", unwrapFieldValue("hello"))
		require.Equal(t, 42.0, unwrapFieldValue(42.0))
		require.Equal(t, true, unwrapFieldValue(true))
		require.Nil(t, unwrapFieldValue(nil))
	})
}

func TestCastToInt(t *testing.T) {
	t.Run("parses numeric values and numeric strings", func(t *testing.T) {
		v, err := castToInt(simplejson.NewFromAny(42))
		require.NoError(t, err)
		require.Equal(t, 42, v)

		v, err = castToInt(simplejson.NewFromAny("42"))
		require.NoError(t, err)
		require.Equal(t, 42, v)
	})

	t.Run("errors on values that are neither int nor numeric string", func(t *testing.T) {
		_, err := castToInt(simplejson.NewFromAny("abc"))
		require.Error(t, err)
	})
}

func TestCastToFloat(t *testing.T) {
	t.Run("parses numeric values and numeric strings", func(t *testing.T) {
		require.Equal(t, 2.5, *castToFloat(simplejson.NewFromAny(2.5)))
		require.Equal(t, 2.5, *castToFloat(simplejson.NewFromAny("2.5")))
	})

	t.Run("returns nil for NaN regardless of case", func(t *testing.T) {
		require.Nil(t, castToFloat(simplejson.NewFromAny("nan")))
		require.Nil(t, castToFloat(simplejson.NewFromAny("NaN")))
	})

	t.Run("returns nil for non-numeric strings", func(t *testing.T) {
		require.Nil(t, castToFloat(simplejson.NewFromAny("abc")))
	})
}
