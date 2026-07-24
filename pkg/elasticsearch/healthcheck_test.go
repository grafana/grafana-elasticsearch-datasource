package elasticsearch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var mockedCfg = config.WithGrafanaConfig(context.Background(), config.NewGrafanaCfg(map[string]string{featuretoggles.EnabledFeatures: "elasticsearchCrossClusterSearch"}))

func Test_Healthcheck_OK(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `{"fields":{"timestamp":{"date":{"metadata_field":true}}}}`)
	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy.", res.Message)
}

func Test_Healthcheck_Timeout(t *testing.T) {
	service := GetMockDatasource(http.StatusRequestTimeout, "408 Request Timeout", `{"status":"red"}`, `{"fields":{"timestamp":{"date":{"metadata_field":true}}}}`)
	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusError, res.Status)
	assert.Equal(t, "Health check failed: Elasticsearch data source is not healthy. Request timed out", res.Message)
}

func Test_Healthcheck_Error(t *testing.T) {
	service := GetMockDatasource(http.StatusBadGateway, "502 Bad Gateway", `{"status":"red"}`, `{"fields":{"timestamp":{"date":{"metadata_field":true}}}}`)
	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusError, res.Status)
	assert.Equal(t, "Health check failed: Elasticsearch data source is not healthy. Status: 502 Bad Gateway", res.Message)
}

func Test_validateIndex_Warning_ErrorValidatingIndex(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `{"error":{"reason":"index_not_found"}}`)
	res, _ := service.CheckHealth(mockedCfg, &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy. Warning: Error validating index: index_not_found", res.Message)
}

func Test_validateIndex_Warning_ErrorValidatingIndex2(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `{"error":"not a map"}`)
	res, _ := service.CheckHealth(mockedCfg, &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy. Warning: Error validating index", res.Message)
}

func Test_validateIndex_Warning_WrongTimestampType(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `{"fields":{"timestamp":{"float":{"metadata_field":true}}}}`)
	res, _ := service.CheckHealth(mockedCfg, &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy. Warning: Could not find time field 'timestamp' with type date in index", res.Message)
}
func Test_validateIndex_Error_FailedToUnmarshalValidateResponse(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `\\\///{"fields":null}"`)
	res, _ := service.CheckHealth(mockedCfg, &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusError, res.Status)
	assert.Equal(t, "Failed to unmarshal field capabilities response", res.Message)
}
func Test_validateIndex_Success_SuccessValidatingIndex(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"green"}`, `{"fields":{"timestamp":{"date":{"metadata_field":true}}}}`)
	res, _ := service.CheckHealth(mockedCfg, &backend.CheckHealthRequest{
		PluginContext: backend.PluginContext{},
		Headers:       nil,
	})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy.", res.Message)
}

const (
	fieldCapsOKResponse       = `{"fields":{"timestamp":{"date":{"metadata_field":true}}}}`
	rootServerlessResponse    = `{"version":{"number":"8.11.0","build_flavor":"serverless"},"tagline":"You Know, for Search"}`
	rootStatefulResponse      = `{"version":{"number":"8.13.0","build_flavor":"default"},"tagline":"You Know, for Search"}`
	clusterHealthGoneResponse = `{"error":"uri [/_cluster/health] with method [GET] exists but is not available when running in serverless mode"}`
)

// newHealthCheckServer starts a mock Elasticsearch server for health check
// tests and records the path of every request it receives.
func newHealthCheckServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *[]string) {
	t.Helper()
	paths := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return server, paths
}

func newHealthCheckDatasource(t *testing.T, url string, clusterInfo es.ClusterInfo) *DataSource {
	t.Helper()
	httpClient, err := httpclient.New(httpclient.Options{})
	require.NoError(t, err)
	return &DataSource{
		info: &es.DatasourceInfo{
			URL:        url,
			HTTPClient: httpClient,
			ConfiguredFields: es.ConfiguredFields{
				TimeField: "timestamp",
			},
			ClusterInfo: clusterInfo,
		},
		logger: log.New(),
	}
}

var serverlessClusterInfo = es.ClusterInfo{
	Version: es.VersionInfo{Number: "8.11.0", BuildFlavor: es.BuildFlavorServerless},
}

func Test_Healthcheck_Serverless_ValidatesDataAccess(t *testing.T) {
	server, paths := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_field_caps":
			_, _ = w.Write([]byte(fieldCapsOKResponse))
		default:
			t.Errorf("unexpected request to %s on a serverless cluster", r.URL.Path)
			w.WriteHeader(http.StatusGone)
		}
	})
	service := newHealthCheckDatasource(t, server.URL, serverlessClusterInfo)

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch Serverless data source is healthy.", res.Message)
	assert.Equal(t, []string{"/_field_caps"}, *paths)
}

func Test_Healthcheck_Serverless_MissingTimeField(t *testing.T) {
	server, _ := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"fields":{}}`))
	})
	service := newHealthCheckDatasource(t, server.URL, serverlessClusterInfo)

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch Serverless data source is healthy. Warning: Could not find field timestamp in index", res.Message)
}

func Test_Healthcheck_Serverless_AuthError(t *testing.T) {
	server, _ := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"reason":"unable to authenticate user"},"status":401}`))
	})
	service := newHealthCheckDatasource(t, server.URL, serverlessClusterInfo)

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusError, res.Status)
	assert.Equal(t, "Error validating index: unable to authenticate user", res.Message)
}

// Reproduces https://github.com/grafana/grafana-elasticsearch-datasource/issues/388:
// serverless detection failed when the instance was created, the health check
// falls through to _cluster/health, and the serverless gateway answers 410
// Gone. The health check must recover by validating data access instead of
// surfacing the raw 410 to the user.
func Test_Healthcheck_ClusterHealthGone_TreatedAsServerless(t *testing.T) {
	server, paths := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"authentication required"}`))
		case "/_cluster/health":
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(clusterHealthGoneResponse))
		case "/_field_caps":
			_, _ = w.Write([]byte(fieldCapsOKResponse))
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	service := newHealthCheckDatasource(t, server.URL, es.ClusterInfo{})

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch Serverless data source is healthy.", res.Message)
	assert.Equal(t, []string{"/", "/_cluster/health", "/_field_caps"}, *paths)
}

func Test_Healthcheck_EmptyClusterInfo_RefetchDetectsServerless(t *testing.T) {
	server, paths := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(rootServerlessResponse))
		case "/_field_caps":
			_, _ = w.Write([]byte(fieldCapsOKResponse))
		default:
			t.Errorf("unexpected request to %s on a serverless cluster", r.URL.Path)
			w.WriteHeader(http.StatusGone)
		}
	})
	service := newHealthCheckDatasource(t, server.URL, es.ClusterInfo{})

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch Serverless data source is healthy.", res.Message)
	assert.NotContains(t, *paths, "/_cluster/health")
}

func Test_Healthcheck_EmptyClusterInfo_RefetchStateful(t *testing.T) {
	server, _ := newHealthCheckServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(rootStatefulResponse))
		case "/_cluster/health":
			_, _ = w.Write([]byte(`{"status":"green"}`))
		default:
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	service := newHealthCheckDatasource(t, server.URL, es.ClusterInfo{})

	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch data source is healthy.", res.Message)
}

type FakeRoundTripper struct {
	statusCode            int
	status                string
	index                 int
	elasticSearchResponse string
	fieldCapsResponse     string
}

func (fakeRoundTripper *FakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var res *http.Response
	if fakeRoundTripper.index == 0 {
		if fakeRoundTripper.statusCode == http.StatusOK {
			res = &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewBufferString(fakeRoundTripper.elasticSearchResponse)),
			}
		} else {
			res = &http.Response{
				StatusCode: fakeRoundTripper.statusCode,
				Status:     fakeRoundTripper.status,
				Body:       io.NopCloser(bytes.NewBufferString(fakeRoundTripper.elasticSearchResponse)),
			}
		}
		fakeRoundTripper.index++
	} else {
		res = &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewBufferString(fakeRoundTripper.fieldCapsResponse)),
		}
	}
	return res, nil
}

func GetMockDatasource(statusCode int, status string, elasticSearchResponse string, fieldCapsResponse string) *DataSource {
	httpClient, _ := httpclient.New(httpclient.Options{})
	httpClient.Transport = &FakeRoundTripper{statusCode: statusCode, status: status, elasticSearchResponse: elasticSearchResponse, fieldCapsResponse: fieldCapsResponse, index: 0}

	dsInfo := es.DatasourceInfo{
		HTTPClient: httpClient,
		ConfiguredFields: es.ConfiguredFields{
			TimeField: "timestamp",
		},
		// Stateful cluster info so the health check does not re-detect the
		// cluster, which would consume the first canned transport response.
		ClusterInfo: es.ClusterInfo{
			Version: es.VersionInfo{Number: "8.0.0", BuildFlavor: "default"},
		},
	}

	return &DataSource{
		info:   &dsInfo,
		logger: log.New(),
	}
}
