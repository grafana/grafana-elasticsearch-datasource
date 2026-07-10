package elasticsearch

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
)

// A malformed Elasticsearch response can contain a hit whose "_source" is not a
// JSON object (e.g. a string or number). The logs path must not panic on the
// unchecked type assertion, matching the defensive handling in the raw data and
// raw document paths.
func TestProcessLogsResponse_NonMapSourceDoesNotPanic(t *testing.T) {
	p := newLogsResponseProcessor(log.New())
	res := &es.SearchResponse{
		Hits: &es.SearchResponseHits{
			Hits: []map[string]interface{}{
				{
					"_id":     "1",
					"_index":  "idx",
					"_source": "not-an-object",
				},
			},
		},
	}
	target := &Query{
		RefID:   "A",
		Metrics: []*MetricAgg{{Type: logsType, Settings: simplejson.New()}},
	}

	var queryRes backend.DataResponse
	require.NotPanics(t, func() {
		err := p.processLogsResponse(res, target, es.ConfiguredFields{TimeField: "@timestamp"}, &queryRes)
		require.NoError(t, err)
	})
	require.Len(t, queryRes.Frames, 1)
}

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
