package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authHeaderRecorder records the Authorization and arbitrary custom headers
// that reach the mock Elasticsearch server. Tests use this to assert that the
// Grafana plugin-SDK middleware stack (Basic auth, API key, SigV4, custom
// headers) is applied to every request even though the refactor now routes
// traffic through the elasticsearch.Client instead of a bespoke transport.
type authHeaderRecorder struct {
	mu      sync.Mutex
	headers http.Header
}

func (r *authHeaderRecorder) capture(h http.Header) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.headers = h.Clone()
}

func (r *authHeaderRecorder) get(name string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.headers.Get(name)
}

// headerInjectingRoundTripper is a stand-in for Grafana's plugin-SDK auth
// middleware. The SDK exposes middleware as http.RoundTripper wrappers, so we
// simulate exactly that shape and verify the header survives the trip through
// the elasticsearch.Client.
type headerInjectingRoundTripper struct {
	inner   http.RoundTripper
	headers http.Header
}

func (rt *headerInjectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for key, values := range rt.headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return rt.inner.RoundTrip(req)
}

// TestAuthMiddlewarePassthrough confirms that http.Client.Transport middleware
// installed by the Grafana plugin SDK is still invoked for every request made
// via the elasticsearch.Client. This is the core contract the refactor needs
// to preserve.
func TestAuthMiddlewarePassthrough(t *testing.T) {
	cases := []struct {
		name    string
		headers http.Header
	}{
		{
			name: "basic auth header",
			headers: http.Header{
				"Authorization": []string{"Basic dXNlcjpwYXNz"},
			},
		},
		{
			name: "api key",
			headers: http.Header{
				"Authorization": []string{"ApiKey abc123"},
			},
		},
		{
			name: "sigv4-style signed headers",
			headers: http.Header{
				"Authorization":        []string{"AWS4-HMAC-SHA256 Credential=..."},
				"X-Amz-Date":           []string{"20260418T123456Z"},
				"X-Amz-Security-Token": []string{"token"},
			},
		},
		{
			name: "arbitrary custom header",
			headers: http.Header{
				"X-Tenant-Id": []string{"tenant-7"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := &authHeaderRecorder{}
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				recorder.capture(r.Header)
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"responses":[]}`))
			}))
			t.Cleanup(func() { ts.Close() })

			innerHTTP := &http.Client{
				Transport: &headerInjectingRoundTripper{
					inner:   ts.Client().Transport,
					headers: tc.headers,
				},
			}
			esCli, err := NewESClient(innerHTTP, ts.URL)
			require.NoError(t, err)

			ds := &DatasourceInfo{
				URL:      ts.URL,
				Database: "logs-*",
				ESClient: esCli,
			}
			c, err := NewClient(context.Background(), ds, log.NewNullLogger())
			require.NoError(t, err)

			_, err = c.ExecuteMultisearch(&MultiSearchRequest{})
			require.NoError(t, err)

			for key, expected := range tc.headers {
				assert.Equal(t, expected[0], recorder.get(key), "header %q should survive round trip", key)
			}
		})
	}
}

// TestMsearchQueryParams asserts the msearch helper emits the right query
// string based on cluster flavor and datasource options.
func TestMsearchQueryParams(t *testing.T) {
	cases := []struct {
		name                string
		mcsr                int64
		includeFrozen       bool
		serverless          bool
		wantMcsr            string
		wantIgnoreThrottled string
	}{
		{
			name:                "non-serverless applies max_concurrent_shard_requests",
			mcsr:                6,
			includeFrozen:       false,
			wantMcsr:            "6",
			wantIgnoreThrottled: "",
		},
		{
			name:                "serverless omits max_concurrent_shard_requests",
			mcsr:                6,
			includeFrozen:       false,
			serverless:          true,
			wantMcsr:            "",
			wantIgnoreThrottled: "",
		},
		{
			name:                "include_frozen emits ignore_throttled=false",
			mcsr:                6,
			includeFrozen:       true,
			wantMcsr:            "6",
			wantIgnoreThrottled: "false",
		},
		{
			name:                "no mcsr configured and not frozen → no params",
			mcsr:                0,
			includeFrozen:       false,
			wantMcsr:            "",
			wantIgnoreThrottled: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var capturedQuery url.Values
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedQuery = r.URL.Query()
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"responses":[]}`))
			}))
			t.Cleanup(func() { ts.Close() })

			info := ClusterInfo{}
			if tc.serverless {
				info.Version.BuildFlavor = BuildFlavorServerless
			}

			ds := &DatasourceInfo{
				URL:                        ts.URL,
				Database:                   "logs-*",
				ESClient:                   newTestESClient(t, ts.Client(), ts.URL),
				MaxConcurrentShardRequests: tc.mcsr,
				IncludeFrozen:              tc.includeFrozen,
				ClusterInfo:                info,
			}
			c, err := NewClient(context.Background(), ds, log.NewNullLogger())
			require.NoError(t, err)

			_, err = c.ExecuteMultisearch(&MultiSearchRequest{})
			require.NoError(t, err)

			require.NotNil(t, capturedQuery)
			assert.Equal(t, tc.wantMcsr, capturedQuery.Get("max_concurrent_shard_requests"))
			assert.Equal(t, tc.wantIgnoreThrottled, capturedQuery.Get("ignore_throttled"))
		})
	}
}

// TestExecuteEsql_Success verifies the happy path: Body is sent, response is
// decoded, endpoint is /_query.
func TestExecuteEsql_Success(t *testing.T) {
	var gotPath string
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = b
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columns":[{"name":"count","type":"long"}],"values":[[42]]}`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := &DatasourceInfo{
		URL:      ts.URL,
		Database: "logs-*",
		ESClient: newTestESClient(t, ts.Client(), ts.URL),
	}
	c, err := NewClient(context.Background(), ds, log.NewNullLogger())
	require.NoError(t, err)

	res, err := c.ExecuteEsql("FROM logs | STATS count = COUNT(*)")
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "/_query", gotPath)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	assert.Equal(t, "FROM logs | STATS count = COUNT(*)", payload["query"])
	require.Len(t, res.Columns, 1)
	assert.Equal(t, "count", res.Columns[0].Name)
}

// TestExecuteEsql_ErrorStatus verifies that 4xx/5xx responses surface as
// DownstreamError with the response body included.
func TestExecuteEsql_ErrorStatus(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(code)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
			}))
			t.Cleanup(func() { ts.Close() })

			ds := &DatasourceInfo{
				URL:      ts.URL,
				Database: "logs-*",
				ESClient: newTestESClient(t, ts.Client(), ts.URL),
			}
			c, err := NewClient(context.Background(), ds, log.NewNullLogger())
			require.NoError(t, err)

			_, err = c.ExecuteEsql("FROM logs")
			require.Error(t, err)
			assert.True(t, backend.IsDownstreamError(err), "ES|QL error responses must be DownstreamError")
			assert.Contains(t, err.Error(), fmt.Sprintf("status %d", code))
			assert.Contains(t, err.Error(), "boom")
		})
	}
}

// TestExecuteEsql_MalformedResponse confirms JSON decode failures return a
// DownstreamError rather than bubbling an un-typed error.
func TestExecuteEsql_MalformedResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not json`))
	}))
	t.Cleanup(func() { ts.Close() })

	ds := &DatasourceInfo{
		URL:      ts.URL,
		Database: "logs-*",
		ESClient: newTestESClient(t, ts.Client(), ts.URL),
	}
	c, err := NewClient(context.Background(), ds, log.NewNullLogger())
	require.NoError(t, err)

	_, err = c.ExecuteEsql("FROM logs")
	require.Error(t, err)
	assert.True(t, backend.IsDownstreamError(err))
}

// TestExecuteMultisearch_ContextCancelled verifies that a cancelled context
// propagates as an error without swallowing it as a DownstreamError. The
// client log path distinguishes cancellation vs error; here we assert the
// returned error wraps context.Canceled so callers can detect it.
func TestExecuteMultisearch_ContextCancelled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client gives up; we never get to write a response.
		<-r.Context().Done()
	}))
	t.Cleanup(func() { ts.Close() })

	ctx, cancel := context.WithCancel(context.Background())

	ds := &DatasourceInfo{
		URL:      ts.URL,
		Database: "logs-*",
		ESClient: newTestESClient(t, ts.Client(), ts.URL),
	}
	c, err := NewClient(ctx, ds, log.NewNullLogger())
	require.NoError(t, err)

	// Cancel almost immediately so the transport aborts the in-flight request.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err = c.ExecuteMultisearch(&MultiSearchRequest{})
	require.Error(t, err)
	// context.Canceled surfaces wrapped somewhere in the error chain.
	assert.Contains(t, err.Error(), "context canceled")
}

// TestNewClient_RejectsNilESClient guards against accidental wiring mistakes
// that would later panic inside esapi.*.Do.
func TestNewClient_RejectsNilESClient(t *testing.T) {
	ds := &DatasourceInfo{
		URL:      "http://localhost:9200",
		Database: "logs-*",
	}
	c, err := NewClient(context.Background(), ds, log.NewNullLogger())
	require.Error(t, err)
	require.Nil(t, c)
	assert.Contains(t, err.Error(), "elasticsearch client is not configured")
}

// TestNewESClient_RejectsNilHTTPClient guards the factory from producing a
// client backed by nil transport.
func TestNewESClient_RejectsNilHTTPClient(t *testing.T) {
	_, err := NewESClient(nil, "http://localhost:9200")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http client is required")
}
