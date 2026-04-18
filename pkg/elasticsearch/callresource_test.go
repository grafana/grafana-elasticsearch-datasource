package elasticsearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

// capturingSender records the single CallResourceResponse the handler sends.
type capturingSender struct {
	resp *backend.CallResourceResponse
}

func (s *capturingSender) Send(r *backend.CallResourceResponse) error {
	s.resp = r
	return nil
}

// newCallResourceTestDatasource wires up a DataSource whose ESClient talks to
// the provided test server. Because the client library performs a product
// check on the first response, the test server MUST emit X-Elastic-Product
// for the request to succeed.
func newCallResourceTestDatasource(t *testing.T, ts *httptest.Server) *DataSource {
	t.Helper()
	esClient, err := es.NewESClient(ts.Client(), ts.URL)
	require.NoError(t, err)
	return &DataSource{
		info:   &es.DatasourceInfo{URL: ts.URL, ESClient: esClient},
		logger: log.New(),
	}
}

func TestDataSource_CallResource_ProxiesMsearch(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	var gotCT string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"responses":[{"status":200}]}`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := newCallResourceTestDatasource(t, ts)

	body := []byte(`{"index":"logs-*"}` + "\n" + `{"query":{"match_all":{}}}` + "\n")
	req := &backend.CallResourceRequest{
		Method: http.MethodPost,
		Path:   "_msearch",
		Body:   body,
		Headers: map[string][]string{
			"Content-Type": {"application/x-ndjson"},
		},
	}

	sender := &capturingSender{}
	err := ds.CallResource(context.Background(), req, sender)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/_msearch", gotPath)
	assert.Equal(t, body, gotBody, "body bytes must be forwarded verbatim")
	assert.Equal(t, "application/x-ndjson", gotCT)

	require.NotNil(t, sender.resp)
	assert.Equal(t, http.StatusOK, sender.resp.Status)
	assert.Equal(t, []string{"application/json"}, sender.resp.Headers["content-type"])
	assert.JSONEq(t, `{"responses":[{"status":200}]}`, string(sender.resp.Body))
}

func TestDataSource_CallResource_ForwardsErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := newCallResourceTestDatasource(t, ts)
	sender := &capturingSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Method: http.MethodGet,
		Path:   "_mapping",
	}, sender)
	require.NoError(t, err)

	require.NotNil(t, sender.resp)
	assert.Equal(t, http.StatusBadRequest, sender.resp.Status)
	assert.JSONEq(t, `{"error":"bad"}`, string(sender.resp.Body))
}

func TestDataSource_CallResource_PreservesContentEncoding(t *testing.T) {
	// "br" (Brotli) is not auto-decoded by Go's http transport, so the
	// Content-Encoding header arrives at CallResource intact and we can
	// verify the handler forwards it verbatim to the sender.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Encoding", "br")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`opaque-bytes`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := newCallResourceTestDatasource(t, ts)
	sender := &capturingSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Method: http.MethodGet,
		Path:   "_mapping",
	}, sender)
	require.NoError(t, err)

	require.NotNil(t, sender.resp)
	assert.Equal(t, []string{"br"}, sender.resp.Headers["content-encoding"])
}

func TestDataSource_CallResource_AddsFieldsQueryForFieldCaps(t *testing.T) {
	var gotURI string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := newCallResourceTestDatasource(t, ts)
	sender := &capturingSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Method: http.MethodPost,
		Path:   "logs/_field_caps",
	}, sender)
	require.NoError(t, err)

	assert.Equal(t, "/logs/_field_caps?fields=*", gotURI)
}

func TestDataSource_CallResource_RejectsInvalidPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("request should not have reached the server, got %s", r.URL.Path)
	}))
	t.Cleanup(func() { ts.Close() })

	ds := newCallResourceTestDatasource(t, ts)
	sender := &capturingSender{}
	err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
		Method: http.MethodDelete,
		Path:   "some/malicious/path",
	}, sender)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid resource URL")
	assert.Nil(t, sender.resp, "no response should be sent when the path is rejected")
}
