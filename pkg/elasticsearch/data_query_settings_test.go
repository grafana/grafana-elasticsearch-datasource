package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringToIntWithDefaultValue(t *testing.T) {
	require.Equal(t, 5, stringToIntWithDefaultValue("5", 10), "parses a valid int")
	require.Equal(t, 10, stringToIntWithDefaultValue("abc", 10), "unparseable falls back to default")
	require.Equal(t, 10, stringToIntWithDefaultValue("0", 10), "zero is treated as invalid")
	require.Equal(t, 10, stringToIntWithDefaultValue("", 10), "empty falls back to default")
}

func TestStringToFloatWithDefaultValue(t *testing.T) {
	require.Equal(t, 2.5, stringToFloatWithDefaultValue("2.5", 1.0), "parses a valid float")
	require.Equal(t, 1.0, stringToFloatWithDefaultValue("abc", 1.0), "unparseable falls back to default")
	require.Equal(t, 1.0, stringToFloatWithDefaultValue("0", 1.0), "zero is treated as invalid")
	require.Equal(t, 1.0, stringToFloatWithDefaultValue("", 1.0), "empty falls back to default")
}
