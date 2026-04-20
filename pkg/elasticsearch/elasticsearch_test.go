package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func unwrapTestDatasource(t *testing.T, instance instancemgmt.Instance) *DataSource {
	t.Helper()
	iw, ok := instance.(*instanceWithSchema)
	require.True(t, ok, "expected *instanceWithSchema")
	return iw.DataSource
}

// contextWithForwardedHeader simulates what the SDK's headerMiddleware does:
// it injects a contextual HTTP client middleware that sets a header on outgoing
// requests — but only when ForwardHTTPHeaders is true on the HTTP client options.
func contextWithForwardedHeader(t *testing.T, key, value string) context.Context {
	t.Helper()
	return httpclient.WithContextualMiddleware(context.Background(),
		httpclient.MiddlewareFunc(func(opts httpclient.Options, next http.RoundTripper) http.RoundTripper {
			if !opts.ForwardHTTPHeaders {
				return next
			}
			return httpclient.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get(key) == "" {
					req.Header.Set(key, value)
				}
				return next.RoundTrip(req)
			})
		}),
	)
}

type datasourceInfo struct {
	TimeField                  any    `json:"timeField"`
	MaxConcurrentShardRequests any    `json:"maxConcurrentShardRequests,omitempty"`
	Interval                   string `json:"interval"`
}

// mockElasticsearchServer creates a test HTTP server that mocks Elasticsearch cluster info endpoint
func mockElasticsearchServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return a mock Elasticsearch cluster info response
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"version": map[string]interface{}{
				"build_flavor": "serverless",
				"number":       "8.0.0",
			},
		})
	}))
}

func TestNewDatasource_ForwardHTTPHeaders(t *testing.T) {
	t.Run("HTTP client forwards OAuth and other HTTP headers from request context", func(t *testing.T) {
		// When oauthPassThru is enabled, the SDK's headerMiddleware puts forwarded
		// headers (Authorization, X-Id-Token, cookies) into the context as a
		// contextual HTTP client middleware. That middleware only fires if the HTTP
		// client was created with ForwardHTTPHeaders: true.
		//
		// Before externalization this was not needed because Grafana's in-process
		// HTTPClientMiddleware forwarded headers unconditionally. After
		// externalization the context is lost over gRPC, so the plugin must opt-in.

		var receivedAuthHeader string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuthHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/" || r.URL.Path == "" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"version": map[string]interface{}{"build_flavor": "default"},
				})
				return
			}
			// Return minimal valid msearch response
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"responses": []interface{}{
					map[string]interface{}{
						"hits": map[string]interface{}{
							"hits": []interface{}{},
						},
						"status": 200,
						"aggregations": map[string]interface{}{
							"2": map[string]interface{}{
								"buckets": []interface{}{},
							},
						},
					},
				},
			})
		}))
		defer server.Close()

		dsSettings := backend.DataSourceInstanceSettings{
			URL: server.URL,
			JSONData: json.RawMessage(`{
				"timeField": "@timestamp",
				"oauthPassThru": true
			}`),
		}

		instance, err := NewDatasource(context.Background(), dsSettings)
		require.NoError(t, err)
		ds := instance.(*DataSource)

		// Simulate the SDK's headerMiddleware: it reads OAuth headers from
		// req.GetHTTPHeaders() and injects them into the context via
		// httpclient.WithContextualMiddleware — but only if the HTTP client
		// opts have ForwardHTTPHeaders: true.
		//
		// We replicate that by injecting a contextual middleware directly,
		// which is exactly what the SDK does at runtime.
		oauthToken := "Bearer test-oauth-token-12345"
		ctx := contextWithForwardedHeader(t, "Authorization", oauthToken)

		query := backend.QueryDataRequest{
			Queries: []backend.DataQuery{
				{
					RefID: "A",
					JSON:  json.RawMessage(`{"metrics":[{"type":"count","id":"1"}],"bucketAggs":[{"type":"date_histogram","id":"2","settings":{"interval":"auto"}}]}`),
				},
			},
		}

		_, err = queryData(ctx, &query, ds.info, log.New())
		require.NoError(t, err)
		require.Equal(t, oauthToken, receivedAuthHeader,
			"OAuth token must be forwarded to Elasticsearch when oauthPassThru is enabled")
	})
}

func TestNewDatasource(t *testing.T) {
	t.Run("fields exist", func(t *testing.T) {
		server := mockElasticsearchServer()
		defer server.Close()

		dsInfo := datasourceInfo{
			TimeField:                  "@timestamp",
			MaxConcurrentShardRequests: 5,
		}
		settingsJSON, err := json.Marshal(dsInfo)
		require.NoError(t, err)

		dsSettings := backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(settingsJSON),
		}

		_, err = NewDatasource(context.Background(), dsSettings)
		require.NoError(t, err)
	})

	t.Run("cluster info fails with 403 - should continue with non-serverless defaults", func(t *testing.T) {
		// Create a server that returns 403 Forbidden (simulating restricted permissions)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		dsInfo := datasourceInfo{
			TimeField:                  "@timestamp",
			MaxConcurrentShardRequests: 5,
		}
		settingsJSON, err := json.Marshal(dsInfo)
		require.NoError(t, err)

		dsSettings := backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(settingsJSON),
		}

		instance, err := NewDatasource(context.Background(), dsSettings)
		require.NoError(t, err)
		require.NotNil(t, instance)

		// Verify that the datasource was created with empty (non-serverless) cluster info
		dsInstance := unwrapTestDatasource(t, instance)
		require.False(t, dsInstance.info.ClusterInfo.IsServerless())
		require.Equal(t, "", dsInstance.info.ClusterInfo.Version.BuildFlavor)
	})

	t.Run("timeField", func(t *testing.T) {
		t.Run("is nil", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				MaxConcurrentShardRequests: 5,
				Interval:                   "Daily",
			}

			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			_, err = NewDatasource(context.Background(), dsSettings)
			require.EqualError(t, err, "timeField cannot be cast to string")
		})

		t.Run("is empty", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				MaxConcurrentShardRequests: 5,
				Interval:                   "Daily",
				TimeField:                  "",
			}

			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			_, err = NewDatasource(context.Background(), dsSettings)
			require.EqualError(t, err, "elasticsearch time field name is required")
		})
	})

	t.Run("maxConcurrentShardRequests", func(t *testing.T) {
		t.Run("no maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField: "@timestamp",
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, defaultMaxConcurrentShardRequests, unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("string maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: "10",
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, int64(10), unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("number maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: 10,
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, int64(10), unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("zero maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: 0,
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, defaultMaxConcurrentShardRequests, unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("negative maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: -10,
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, defaultMaxConcurrentShardRequests, unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("float maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: 10.5,
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, int64(10), unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})

		t.Run("invalid maxConcurrentShardRequests", func(t *testing.T) {
			server := mockElasticsearchServer()
			defer server.Close()

			dsInfo := datasourceInfo{
				TimeField:                  "@timestamp",
				MaxConcurrentShardRequests: "invalid",
			}
			settingsJSON, err := json.Marshal(dsInfo)
			require.NoError(t, err)

			dsSettings := backend.DataSourceInstanceSettings{
				URL:      server.URL,
				JSONData: json.RawMessage(settingsJSON),
			}

			instance, err := NewDatasource(context.Background(), dsSettings)
			require.Equal(t, defaultMaxConcurrentShardRequests, unwrapTestDatasource(t, instance).info.MaxConcurrentShardRequests)
			require.NoError(t, err)
		})
	})
}

func TestCreateElasticsearchURL(t *testing.T) {
	tt := []struct {
		name     string
		settings es.DatasourceInfo
		req      backend.CallResourceRequest
		expected string
	}{
		{name: "with /_msearch path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "_msearch"}, expected: "http://localhost:9200/_msearch"},
		{name: "with _msearch path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "_msearch"}, expected: "http://localhost:9200/_msearch"},
		{name: "with _msearch path and valid url with /", settings: es.DatasourceInfo{URL: "http://localhost:9200/"}, req: backend.CallResourceRequest{Path: "_msearch"}, expected: "http://localhost:9200/_msearch"},
		{name: "with _mapping path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "/_mapping"}, expected: "http://localhost:9200/_mapping"},
		{name: "with /_mapping path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "/_mapping"}, expected: "http://localhost:9200/_mapping"},
		{name: "with /_mapping path and valid url with /", settings: es.DatasourceInfo{URL: "http://localhost:9200/"}, req: backend.CallResourceRequest{Path: "/_mapping"}, expected: "http://localhost:9200/_mapping"},
		{name: "with abc/_mapping path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "abc/_mapping"}, expected: "http://localhost:9200/abc/_mapping"},
		{name: "with /abc/_mapping path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: "abc/_mapping"}, expected: "http://localhost:9200/abc/_mapping"},
		{name: "with /abc/_mapping path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200/"}, req: backend.CallResourceRequest{Path: "abc/_mapping"}, expected: "http://localhost:9200/abc/_mapping"},
		// This is to support mapping for cluster searches that include ":"
		{name: "with path including :", settings: es.DatasourceInfo{URL: "http://localhost:9200/"}, req: backend.CallResourceRequest{Path: "ab:c/_mapping"}, expected: "http://localhost:9200/ab:c/_mapping"},
		{name: "with \"\" path and valid url and /", settings: es.DatasourceInfo{URL: "http://localhost:9200/"}, req: backend.CallResourceRequest{Path: ""}, expected: "http://localhost:9200/"},
		{name: "with \"\" path and valid url", settings: es.DatasourceInfo{URL: "http://localhost:9200"}, req: backend.CallResourceRequest{Path: ""}, expected: "http://localhost:9200/"},
		{name: "with \"\" path and valid url with path", settings: es.DatasourceInfo{URL: "http://elastic:9200/lb"}, req: backend.CallResourceRequest{Path: ""}, expected: "http://elastic:9200/lb/"},
		{name: "with \"\" path and valid url with path and /", settings: es.DatasourceInfo{URL: "http://elastic:9200/lb/"}, req: backend.CallResourceRequest{Path: ""}, expected: "http://elastic:9200/lb/"},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			url, err := createElasticsearchURL(&test.req, &test.settings)
			require.NoError(t, err)
			require.Equal(t, test.expected, url)
		})
	}
}
