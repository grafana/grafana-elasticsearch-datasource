package elasticsearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/instrumentation"
)

func unwrapTestDatasource(t *testing.T, instance instancemgmt.Instance) *DataSource {
	t.Helper()
	ds, ok := instance.(*DataSource)
	require.True(t, ok, "expected *DataSource")
	return ds
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
		// The official go-elasticsearch client rejects any response that does
		// not carry this product header, so a mock standing in for a real
		// cluster has to send it too.
		w.Header().Set("X-Elastic-Product", "Elasticsearch")
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
		require.False(t, dsInstance.info.ClusterInfo().IsServerless())
		require.Equal(t, "", dsInstance.info.ClusterInfo().Version.BuildFlavor)
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

func TestDispose(t *testing.T) {
	t.Run("closes idle connections on the underlying HTTP client", func(t *testing.T) {
		server := mockElasticsearchServer()
		defer server.Close()

		dsInfo := datasourceInfo{
			TimeField:                  "@timestamp",
			MaxConcurrentShardRequests: 5,
		}
		settingsJSON, err := json.Marshal(dsInfo)
		require.NoError(t, err)

		instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(settingsJSON),
		})
		require.NoError(t, err)

		ds := unwrapTestDatasource(t, instance)

		// Warm an idle connection by issuing a request through the client.
		resp, err := ds.info.HTTPClient.Get(server.URL)
		require.NoError(t, err)
		_ = resp.Body.Close()

		require.NotPanics(t, func() { ds.Dispose() })
	})

	t.Run("is safe to call when HTTPClient is nil", func(t *testing.T) {
		ds := &DataSource{info: &es.DatasourceInfo{}}
		require.NotPanics(t, func() { ds.Dispose() })
	})

	t.Run("is safe to call when info is nil", func(t *testing.T) {
		ds := &DataSource{}
		require.NotPanics(t, func() { ds.Dispose() })
	})
}

func gaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, gauge.Write(&m))
	return m.GetGauge().GetValue()
}

func TestDatasourceInstanceGauge(t *testing.T) {
	t.Run("NewDatasource increments the instance gauge and Dispose decrements it", func(t *testing.T) {
		// mockElasticsearchServer reports build_flavor serverless and version 8.0.0
		server := mockElasticsearchServer()
		defer server.Close()

		gauge := instrumentation.DatasourceInstances.WithLabelValues(es.DistributionElasticsearchServerless, "8")
		before := gaugeValue(t, gauge)

		instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(`{"timeField": "@timestamp"}`),
		})
		require.NoError(t, err)
		require.Equal(t, before+1, gaugeValue(t, gauge))

		ds := unwrapTestDatasource(t, instance)
		ds.Dispose()
		require.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("gauge reports unknown labels when the root endpoint is not accessible", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		gauge := instrumentation.DatasourceInstances.WithLabelValues(es.DistributionUnknown, es.DistributionUnknown)
		before := gaugeValue(t, gauge)

		instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(`{"timeField": "@timestamp"}`),
		})
		require.NoError(t, err)
		require.Equal(t, before+1, gaugeValue(t, gauge))

		ds := unwrapTestDatasource(t, instance)
		ds.Dispose()
		require.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("gauge bounds labels when the cluster reports garbage version info", func(t *testing.T) {
		// A remote endpoint controls the distribution and version strings in
		// the root response. Unbounded values would mint a permanent gauge
		// series per distinct pair, so they must collapse to bounded labels.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// The official SDK rejects servers that omit this header, so a
			// server must claim to be Elasticsearch to be classified at all;
			// the garbage version number must still collapse to a bounded label.
			w.Header().Set("X-Elastic-Product", "Elasticsearch")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"version": map[string]interface{}{
					"distribution": "Elasticsearch<script>",
					"number":       "banana",
				},
			})
		}))
		defer server.Close()

		gauge := instrumentation.DatasourceInstances.WithLabelValues("elasticsearch", "unknown")
		before := gaugeValue(t, gauge)

		instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(`{"timeField": "@timestamp"}`),
		})
		require.NoError(t, err)
		require.Equal(t, before+1, gaugeValue(t, gauge))

		ds := unwrapTestDatasource(t, instance)
		ds.Dispose()
		require.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("Dispose does not decrement for instances that never registered", func(t *testing.T) {
		gauge := instrumentation.DatasourceInstances.WithLabelValues(es.DistributionUnknown, es.DistributionUnknown)
		before := gaugeValue(t, gauge)

		ds := &DataSource{info: &es.DatasourceInfo{}}
		ds.Dispose()

		require.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("repeated Dispose decrements the gauge only once", func(t *testing.T) {
		// The SDK schedules Dispose on a cached instance before attempting its
		// replacement. When constructing the replacement fails, the old
		// instance stays cached and a later settings change schedules Dispose
		// on it again.
		server := mockElasticsearchServer()
		defer server.Close()

		gauge := instrumentation.DatasourceInstances.WithLabelValues(es.DistributionElasticsearchServerless, "8")
		before := gaugeValue(t, gauge)

		instance, err := NewDatasource(context.Background(), backend.DataSourceInstanceSettings{
			URL:      server.URL,
			JSONData: json.RawMessage(`{"timeField": "@timestamp"}`),
		})
		require.NoError(t, err)

		ds := unwrapTestDatasource(t, instance)
		ds.Dispose()
		ds.Dispose()
		require.Equal(t, before, gaugeValue(t, gauge))
	})
}

func TestBuildCallResourceRequest(t *testing.T) {
	tt := []struct {
		name             string
		req              backend.CallResourceRequest
		expectedPath     string
		expectedRawQuery string
	}{
		{name: "with _msearch path", req: backend.CallResourceRequest{Path: "_msearch", Method: http.MethodPost}, expectedPath: "/_msearch"},
		{name: "with /_msearch path", req: backend.CallResourceRequest{Path: "/_msearch", Method: http.MethodPost}, expectedPath: "/_msearch"},
		{name: "with _mapping path", req: backend.CallResourceRequest{Path: "_mapping", Method: http.MethodGet}, expectedPath: "/_mapping"},
		{name: "with /_mapping path", req: backend.CallResourceRequest{Path: "/_mapping", Method: http.MethodGet}, expectedPath: "/_mapping"},
		{name: "with abc/_mapping path", req: backend.CallResourceRequest{Path: "abc/_mapping", Method: http.MethodGet}, expectedPath: "/abc/_mapping"},
		// Paths with ":" are used for cross-cluster search
		{name: "with path including :", req: backend.CallResourceRequest{Path: "ab:c/_mapping", Method: http.MethodGet}, expectedPath: "/ab:c/_mapping"},
		{name: "with empty path", req: backend.CallResourceRequest{Path: "", Method: http.MethodGet}, expectedPath: "/"},
		{name: "with _field_caps path adds fields=* query", req: backend.CallResourceRequest{Path: "_field_caps", Method: http.MethodPost}, expectedPath: "/_field_caps", expectedRawQuery: "fields=*"},
		{name: "with abc/_field_caps path adds fields=* query", req: backend.CallResourceRequest{Path: "abc/_field_caps", Method: http.MethodPost}, expectedPath: "/abc/_field_caps", expectedRawQuery: "fields=*"},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			request, err := buildCallResourceRequest(context.Background(), &test.req)
			require.NoError(t, err)
			require.Equal(t, test.expectedPath, request.URL.Path)
			require.Equal(t, test.expectedRawQuery, request.URL.RawQuery)
			require.Equal(t, test.req.Method, request.Method)
		})
	}
}

// TestCallResource_URLComposition verifies that the Elasticsearch client
// correctly combines the configured base URL (scheme, host, base path) with
// the relative path built by buildCallResourceRequest.
func TestCallResource_URLComposition(t *testing.T) {
	tt := []struct {
		name         string
		baseURL      string
		req          backend.CallResourceRequest
		expectedURL  string
		expectedBody string
	}{
		{name: "root base URL + _msearch", baseURL: "", req: backend.CallResourceRequest{Path: "_msearch", Method: http.MethodPost}, expectedURL: "/_msearch"},
		{name: "root base URL + /_msearch", baseURL: "", req: backend.CallResourceRequest{Path: "/_msearch", Method: http.MethodPost}, expectedURL: "/_msearch"},
		{name: "root base URL + abc/_mapping", baseURL: "", req: backend.CallResourceRequest{Path: "abc/_mapping", Method: http.MethodGet}, expectedURL: "/abc/_mapping"},
		{name: "root base URL + empty path", baseURL: "", req: backend.CallResourceRequest{Path: "", Method: http.MethodGet}, expectedURL: "/"},
		{name: "root base URL + cross-cluster path", baseURL: "", req: backend.CallResourceRequest{Path: "ab:c/_mapping", Method: http.MethodGet}, expectedURL: "/ab:c/_mapping"},
		{name: "root base URL + _field_caps adds fields=*", baseURL: "", req: backend.CallResourceRequest{Path: "_field_caps", Method: http.MethodPost}, expectedURL: "/_field_caps?fields=*"},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			var gotURL string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotURL = r.URL.RequestURI()
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer ts.Close()

			esClient, err := es.NewESClient(ts.Client(), ts.URL+test.baseURL)
			require.NoError(t, err)

			request, err := buildCallResourceRequest(context.Background(), &test.req)
			require.NoError(t, err)

			resp, err := esClient.Perform(request)
			require.NoError(t, err)
			_ = resp.Body.Close()

			require.Equal(t, test.expectedURL, gotURL)
		})
	}
}

func TestIsAllowedResourcePath(t *testing.T) {
	tt := []struct {
		name    string
		path    string
		allowed bool
	}{
		{name: "empty path (db version)", path: "", allowed: true},
		{name: "_field_caps", path: "_field_caps", allowed: true},
		{name: "index/_field_caps", path: "myindex/_field_caps", allowed: true},
		{name: "_msearch", path: "_msearch", allowed: true},
		{name: "_mapping", path: "_mapping", allowed: true},
		{name: "index/_mapping", path: "myindex/_mapping", allowed: true},
		{name: "query string on an otherwise-allowed path", path: "myindex/_mapping?pretty=true", allowed: true},
		{name: "unrelated path", path: "_cluster/settings", allowed: false},
		{name: "dot segment", path: "myindex/./_mapping", allowed: false},
		{name: "dot-dot segment escaping the base path", path: "../otherindex/_mapping", allowed: false},
		{name: "dot-dot segment in the middle", path: "myindex/../otherindex/_mapping", allowed: false},
		{name: "bare dot-dot segment", path: "..", allowed: false},
		{name: "literal percent sign", path: "my%2findex/_mapping", allowed: false},
		{name: "literal percent sign confined to the query string is fine, path is unaffected", path: "myindex/_mapping?q=100%25", allowed: true},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.allowed, isAllowedResourcePath(test.path))
		})
	}
}

func TestIsAllowedMethod(t *testing.T) {
	tt := []struct {
		name    string
		path    string
		method  string
		allowed bool
	}{
		{name: "_msearch via POST", path: "_msearch", method: http.MethodPost, allowed: true},
		{name: "_msearch via GET", path: "_msearch", method: http.MethodGet, allowed: false},
		{name: "_msearch via PUT", path: "_msearch", method: http.MethodPut, allowed: false},
		{name: "_mapping via GET", path: "_mapping", method: http.MethodGet, allowed: true},
		{name: "_mapping via PUT (the reported write)", path: "_mapping", method: http.MethodPut, allowed: false},
		{name: "index/_mapping via POST", path: "myindex/_mapping", method: http.MethodPost, allowed: false},
		{name: "_field_caps via GET", path: "_field_caps", method: http.MethodGet, allowed: true},
		{name: "_field_caps via POST", path: "_field_caps", method: http.MethodPost, allowed: false},
		{name: "empty path via GET", path: "", method: http.MethodGet, allowed: true},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.allowed, isAllowedMethod(test.path, test.method))
		})
	}
}

// Test_CallResource_rejectsDisallowedPathsAndMethods exercises the checks through the real
// handler rather than the helper functions in isolation, confirming a rejected call never
// reaches the upstream Elasticsearch request.
func Test_CallResource_rejectsDisallowedPathsAndMethods(t *testing.T) {
	tt := []struct {
		name   string
		path   string
		method string
	}{
		{name: "write to _mapping", path: "myindex/_mapping", method: http.MethodPut},
		{name: "path traversal out of the configured index scope", path: "../otherindex/_mapping", method: http.MethodGet},
		{name: "double-encoded traversal", path: "my%2findex/_mapping", method: http.MethodGet},
		{name: "GET on _msearch", path: "_msearch", method: http.MethodGet},
		{name: "unrelated cluster-settings path", path: "_cluster/settings", method: http.MethodGet},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			var upstreamWasHit bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamWasHit = true
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			ds := newCallResourceTestDatasource(t, upstream)

			err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
				Path:   test.path,
				Method: test.method,
			}, backend.CallResourceResponseSenderFunc(func(*backend.CallResourceResponse) error {
				return nil
			}))

			require.Error(t, err)
			require.False(t, upstreamWasHit, "rejected request must never reach Elasticsearch")
		})
	}
}

// Test_CallResource_forwardsAllowedCalls confirms the allowlist doesn't reject the calls this
// plugin actually issues.
func Test_CallResource_forwardsAllowedCalls(t *testing.T) {
	tt := []struct {
		name   string
		path   string
		method string
	}{
		{name: "GET _mapping", path: "_mapping", method: http.MethodGet},
		{name: "GET index/_mapping", path: "myindex/_mapping", method: http.MethodGet},
		{name: "GET _field_caps", path: "_field_caps", method: http.MethodGet},
		{name: "POST _msearch", path: "_msearch", method: http.MethodPost},
		{name: "GET empty path (db version)", path: "", method: http.MethodGet},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			var upstreamWasHit bool
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamWasHit = true
				w.Header().Set("X-Elastic-Product", "Elasticsearch")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer upstream.Close()

			ds := newCallResourceTestDatasource(t, upstream)

			var captured *backend.CallResourceResponse
			err := ds.CallResource(context.Background(), &backend.CallResourceRequest{
				Path:   test.path,
				Method: test.method,
			}, backend.CallResourceResponseSenderFunc(func(resp *backend.CallResourceResponse) error {
				captured = resp
				return nil
			}))

			require.NoError(t, err)
			require.True(t, upstreamWasHit)
			require.Equal(t, http.StatusOK, captured.Status)
		})
	}
}
