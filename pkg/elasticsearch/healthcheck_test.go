package elasticsearch

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	"github.com/stretchr/testify/assert"
)

var mockedCfg = backend.WithGrafanaConfig(context.Background(), backend.NewGrafanaCfg(map[string]string{featuretoggles.EnabledFeatures: "elasticsearchCrossClusterSearch"}))

// Test_Healthcheck_Serverless verifies the short-circuit: when the cluster
// reports a serverless build flavor we skip the /_cluster/health call entirely
// and return healthy. Regression guard: a bug in this shortcut would send
// hundreds of requests to a serverless cluster that doesn't support the
// endpoint.
func Test_Healthcheck_Serverless(t *testing.T) {
	// Handler panics if reached — serverless shortcut should skip the HTTP call.
	httpClient, _ := httpclient.New(httpclient.Options{})
	httpClient.Transport = &failingRoundTripper{t: t}
	esClient, _ := es.NewESClient(httpClient, "http://localhost:9200")

	ds := &DataSource{
		info: &es.DatasourceInfo{
			ESClient: esClient,
			ClusterInfo: es.ClusterInfo{
				Version: es.VersionInfo{BuildFlavor: es.BuildFlavorServerless},
			},
		},
		logger: log.New(),
	}

	res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.NoError(t, err)
	assert.Equal(t, backend.HealthStatusOk, res.Status)
	assert.Equal(t, "Elasticsearch Serverless data source is healthy.", res.Message)
}

// Test_Healthcheck_RedStatus verifies the response body-level red status path
// (distinct from non-2xx HTTP status codes already covered above).
func Test_Healthcheck_RedStatus(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `{"status":"red"}`, `{}`)
	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusError, res.Status)
	assert.Contains(t, res.Message, "not healthy")
}

// Test_Healthcheck_MalformedBody verifies that a non-JSON body yields an
// unknown status with a truncated snippet echoed back for debugging.
func Test_Healthcheck_MalformedBody(t *testing.T) {
	service := GetMockDatasource(http.StatusOK, "200 OK", `not-json-here`, `{}`)
	res, _ := service.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	assert.Equal(t, backend.HealthStatusUnknown, res.Status)
	assert.Contains(t, res.Message, "Failed to parse response")
	assert.Contains(t, res.Message, "not-json-here")
}

// failingRoundTripper fails the test immediately if any request reaches it.
// Used to assert control flow that should not make an HTTP call.
type failingRoundTripper struct{ t *testing.T }

func (f *failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Fatal("unexpected HTTP request — serverless shortcut should skip it")
	return nil, nil
}

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
	// Satisfy the go-elasticsearch client's product check.
	res.Header = http.Header{}
	res.Header.Set("X-Elastic-Product", "Elasticsearch")
	return res, nil
}

func GetMockDatasource(statusCode int, status string, elasticSearchResponse string, fieldCapsResponse string) *DataSource {
	httpClient, _ := httpclient.New(httpclient.Options{})
	httpClient.Transport = &FakeRoundTripper{statusCode: statusCode, status: status, elasticSearchResponse: elasticSearchResponse, fieldCapsResponse: fieldCapsResponse, index: 0}

	esClient, _ := es.NewESClient(httpClient, "http://localhost:9200")

	dsInfo := es.DatasourceInfo{
		ESClient: esClient,
		ConfiguredFields: es.ConfiguredFields{
			TimeField: "timestamp",
		},
	}

	return &DataSource{
		info:   &dsInfo,
		logger: log.New(),
	}
}
