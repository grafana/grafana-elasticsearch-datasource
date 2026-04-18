package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-aws-sdk/pkg/awsauth"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

const (
	// headerFromExpression is used by data sources to identify expression queries
	headerFromExpression = "X-Grafana-From-Expr"
	// headerFromAlert is used by data sources to identify alert queries
	headerFromAlert = "FromAlert"
	// this is the default value for the maxConcurrentShardRequests setting - it should be in sync with the default value in the datasource config settings
	defaultMaxConcurrentShardRequests = int64(5)
)

type DataSource struct {
	info   *es.DatasourceInfo
	logger log.Logger
}

func (ds *DataSource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	_, fromAlert := req.Headers[headerFromAlert]
	logger := ds.logger.FromContext(ctx).With("fromAlert", fromAlert)

	return queryData(ctx, req, ds.info, logger)
}

// separate function to allow testing the whole transformation and query flow
func queryData(ctx context.Context, req *backend.QueryDataRequest, dsInfo *es.DatasourceInfo, logger log.Logger) (*backend.QueryDataResponse, error) {
	if len(req.Queries) == 0 {
		return &backend.QueryDataResponse{}, fmt.Errorf("query contains no queries")
	}

	client, err := es.NewClient(ctx, dsInfo, logger)
	if err != nil {
		return &backend.QueryDataResponse{}, err
	}
	query := newElasticsearchDataQuery(ctx, client, req, logger, dsInfo.Database)
	return query.execute()
}

func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	jsonData := map[string]any{}
	err := json.Unmarshal(settings.JSONData, &jsonData)
	if err != nil {
		return nil, fmt.Errorf("error reading settings: %w", err)
	}
	httpCliOpts, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting http options: %w", err)
	}

	httpCliOpts.ForwardHTTPHeaders = true

	// Set SigV4 service namespace
	if httpCliOpts.SigV4 != nil {
		httpCliOpts.SigV4.Service = "es"
		httpCliOpts.Middlewares = append(httpCliOpts.Middlewares, awsauth.NewSigV4Middleware())
	}

	apiKeyAuth, ok := jsonData["apiKeyAuth"].(bool)
	if ok && apiKeyAuth {
		apiKey := settings.DecryptedSecureJSONData["apiKey"]
		if apiKey != "" {
			httpCliOpts.Header.Add("Authorization", "ApiKey "+apiKey)
		}
	}

	httpCli, err := httpclient.NewProvider().New(httpCliOpts)
	if err != nil {
		return nil, err
	}

	esClient, err := es.NewESClient(httpCli, settings.URL)
	if err != nil {
		return nil, fmt.Errorf("error building elasticsearch client: %w", err)
	}

	// we used to have a field named `esVersion`, please do not use this name in the future.

	timeField, ok := jsonData["timeField"].(string)
	if !ok {
		return nil, backend.DownstreamError(errors.New("timeField cannot be cast to string"))
	}

	if timeField == "" {
		return nil, backend.DownstreamError(errors.New("elasticsearch time field name is required"))
	}

	logLevelField, ok := jsonData["logLevelField"].(string)
	if !ok {
		logLevelField = ""
	}

	logMessageField, ok := jsonData["logMessageField"].(string)
	if !ok {
		logMessageField = ""
	}

	interval, ok := jsonData["interval"].(string)
	if !ok {
		interval = ""
	}

	index, ok := jsonData["index"].(string)
	if !ok {
		index = ""
	}
	if index == "" {
		index = settings.Database
	}

	var maxConcurrentShardRequests int64

	switch v := jsonData["maxConcurrentShardRequests"].(type) {
	// unmarshalling from JSON will return float64 for numbers, so we need to handle that and convert to int64
	case float64:
		maxConcurrentShardRequests = int64(v)
	case string:
		maxConcurrentShardRequests, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			maxConcurrentShardRequests = defaultMaxConcurrentShardRequests
		}
	default:
		maxConcurrentShardRequests = defaultMaxConcurrentShardRequests
	}

	if maxConcurrentShardRequests <= 0 {
		maxConcurrentShardRequests = defaultMaxConcurrentShardRequests
	}

	includeFrozen, ok := jsonData["includeFrozen"].(bool)
	if !ok {
		includeFrozen = false
	}

	clusterInfo, err := es.GetClusterInfo(ctx, esClient)
	if err != nil {
		// Log warning but continue with default (non-serverless) behavior
		// This handles cases where users don't have permission to access the root endpoint (403)
		// or other connectivity issues that shouldn't prevent basic datasource functionality
		backend.Logger.Warn("Failed to get Elasticsearch cluster info, assuming non-serverless cluster", "error", err, "url", settings.URL)
		clusterInfo = es.ClusterInfo{}
	}

	configuredFields := es.ConfiguredFields{
		TimeField:       timeField,
		LogLevelField:   logLevelField,
		LogMessageField: logMessageField,
	}

	model := es.DatasourceInfo{
		ID:                         settings.ID,
		URL:                        settings.URL,
		ESClient:                   esClient,
		Database:                   index,
		MaxConcurrentShardRequests: maxConcurrentShardRequests,
		ConfiguredFields:           configuredFields,
		Interval:                   interval,
		IncludeFrozen:              includeFrozen,
		ClusterInfo:                clusterInfo,
	}
	return &DataSource{
		info:   &model,
		logger: log.New().FromContext(ctx),
	}, nil
}

func isFieldCaps(url string) bool {
	return strings.HasSuffix(url, "/_field_caps") || url == "_field_caps"
}

func (ds *DataSource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	logger := ds.logger.FromContext(ctx)
	// allowed paths for resource calls:
	// - empty string for fetching db version
	// - /_mapping for fetching index mapping, e.g. requests going to `index/_mapping`
	// - /_field_caps for fetching field capabilities, e.g. requests going to `index/_field_caps`
	// - _msearch for executing getTerms queries
	// - _mapping for fetching "root" index mappings
	// - _field_caps for fetching "root" field capabilities
	if req.Path != "" && !isFieldCaps(req.Path) && req.Path != "_msearch" &&
		!strings.HasSuffix(req.Path, "/_mapping") && req.Path != "_mapping" {
		logger.Error("Invalid resource path", "path", req.Path)
		return fmt.Errorf("invalid resource URL: %s", req.Path)
	}

	request, err := buildCallResourceRequest(ctx, req)
	if err != nil {
		logger.Error("Failed to create request", "error", err, "path", req.Path)
		return err
	}

	logger.Debug("Sending request to Elasticsearch", "resourcePath", req.Path)
	start := time.Now()
	response, err := ds.info.ESClient.Perform(request)
	if err != nil {
		status := "error"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		lp := []any{"error", err, "status", status, "duration", time.Since(start), "stage", es.StageDatabaseRequest, "resourcePath", req.Path}
		sourceErr := backend.ErrorWithSource{}
		if errors.As(err, &sourceErr) {
			lp = append(lp, "statusSource", sourceErr.ErrorSource())
		}
		if response != nil {
			lp = append(lp, "statusCode", response.StatusCode)
		}
		logger.Error("Error received from Elasticsearch", lp...)
		return err
	}
	logger.Info("Response received from Elasticsearch", "statusCode", response.StatusCode, "status", "ok", "duration", time.Since(start), "stage", es.StageDatabaseRequest, "contentLength", response.Header.Get("Content-Length"), "resourcePath", req.Path)

	defer func() {
		if err := response.Body.Close(); err != nil {
			logger.Warn("Failed to close response body", "error", err)
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		logger.Error("Error reading response body bytes", "error", err)
		return err
	}

	responseHeaders := map[string][]string{
		"content-type": {"application/json"},
	}

	if response.Header.Get("Content-Encoding") != "" {
		responseHeaders["content-encoding"] = []string{response.Header.Get("Content-Encoding")}
	}

	return sender.Send(&backend.CallResourceResponse{
		Status:  response.StatusCode,
		Headers: responseHeaders,
		Body:    body,
	})
}

// buildCallResourceRequest prepares an *http.Request whose path and query are
// sufficient for the elasticsearch.Client transport to complete with the
// configured cluster address. The transport prepends the address's scheme,
// host and base path when Perform is called.
func buildCallResourceRequest(ctx context.Context, req *backend.CallResourceRequest) (*http.Request, error) {
	// path.Join collapses empty segments and strips trailing slashes. Preserve
	// the previous behaviour by defaulting to "/" when the caller sends an
	// empty path (version sniff against the root endpoint).
	reqPath := "/" + strings.TrimPrefix(req.Path, "/")
	if req.Path == "" {
		reqPath = "/"
	}

	u := &url.URL{Path: reqPath}
	if isFieldCaps(req.Path) {
		u.RawQuery = "fields=*"
	}

	request, err := http.NewRequestWithContext(ctx, req.Method, u.String(), bytes.NewBuffer(req.Body))
	if err != nil {
		return nil, err
	}

	if ct := req.GetHTTPHeader("Content-Type"); ct != "" {
		request.Header.Set("Content-Type", ct)
	}

	return request, nil
}
