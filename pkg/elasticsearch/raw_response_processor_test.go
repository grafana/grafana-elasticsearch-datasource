package elasticsearch

import (
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// A malformed Elasticsearch response can contain a hit whose "_source" is not a
// JSON object (e.g. a string or number). The raw data path must not panic on the
// unchecked type assertion, matching the defensive handling already used by the
// raw document path.
func TestProcessRawDataResponse_NonMapSourceDoesNotPanic(t *testing.T) {
	p := newRawResponseProcessor(log.New())
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

	var queryRes backend.DataResponse
	require.NotPanics(t, func() {
		err := p.processRawDataResponse(res, &Query{RefID: "A"}, es.ConfiguredFields{TimeField: "@timestamp"}, &queryRes)
		require.NoError(t, err)
	})
	require.Len(t, queryRes.Frames, 1)
}
