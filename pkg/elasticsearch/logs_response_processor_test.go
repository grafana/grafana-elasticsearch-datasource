package elasticsearch

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"
)

func TestSetLogsCustomMeta(t *testing.T) {
	frame := data.NewFrame("")
	searchWords := map[string]bool{"foo": true, "bar": true, "baz": true}

	setLogsCustomMeta(frame, searchWords, 100, 5)

	require.NotNil(t, frame.Meta)
	custom, ok := frame.Meta.Custom.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, []string{"bar", "baz", "foo"}, custom["searchWords"], "search words must be sorted")
	require.Equal(t, 100, custom["limit"])
	require.Equal(t, 5, custom["total"])
}
