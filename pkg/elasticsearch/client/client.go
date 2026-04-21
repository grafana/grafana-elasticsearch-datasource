package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
)

// Used in logging to mark a stage
const (
	StagePrepareRequest  = "prepareRequest"
	StageDatabaseRequest = "databaseRequest"
	StageParseResponse   = "parseResponse"
)

type DatasourceInfo struct {
	ID                         int64
	ESClient                   *elasticsearch.Client
	URL                        string
	Database                   string
	ConfiguredFields           ConfiguredFields
	Interval                   string
	MaxConcurrentShardRequests int64
	IncludeFrozen              bool
	ClusterInfo                ClusterInfo
}

type ConfiguredFields struct {
	TimeField       string
	LogMessageField string
	LogLevelField   string
}

// Client represents a client which can interact with elasticsearch api
type Client interface {
	GetConfiguredFields() ConfiguredFields
	ExecuteMultisearch(r *MultiSearchRequest) (*MultiSearchResponse, error)
	MultiSearch() *MultiSearchRequestBuilder
	ExecuteEsql(query string) (*EsqlResponse, error)
}

// NewClient creates a new elasticsearch client
var NewClient = func(ctx context.Context, ds *DatasourceInfo, logger log.Logger) (Client, error) {
	logger = logger.FromContext(ctx).With("entity", "client")

	ip, err := NewIndexPattern(ds.Interval, ds.Database)
	if err != nil {
		logger.Error("Failed creating index pattern", "error", err, "interval", ds.Interval, "index", ds.Database)
		return nil, err
	}

	logger.Debug("Creating new client", "configuredFields", fmt.Sprintf("%#v", ds.ConfiguredFields), "interval", ds.Interval, "index", ds.Database)

	if ds.ESClient == nil {
		return nil, fmt.Errorf("elasticsearch client is not configured on datasource")
	}

	return &baseClientImpl{
		logger:           logger,
		ctx:              ctx,
		ds:               ds,
		configuredFields: ds.ConfiguredFields,
		indexPattern:     ip,
		encoder:          newRequestEncoder(logger),
		parser:           newResponseParser(logger),
	}, nil
}

type baseClientImpl struct {
	ctx              context.Context
	ds               *DatasourceInfo
	configuredFields ConfiguredFields
	indexPattern     IndexPattern
	logger           log.Logger
	encoder          *requestEncoder
	parser           *responseParser
}

func (c *baseClientImpl) GetConfiguredFields() ConfiguredFields {
	return c.configuredFields
}

type multiRequest struct {
	header   map[string]any
	body     any
	interval time.Duration
}

func (c *baseClientImpl) ExecuteMultisearch(r *MultiSearchRequest) (*MultiSearchResponse, error) {
	var err error
	multiRequests, err := c.createMultiSearchRequests(r.Requests)
	if err != nil {
		return nil, err
	}

	payload, err := c.encoder.encodeBatchRequests(multiRequests)
	if err != nil {
		return nil, err
	}

	req := c.buildMsearchRequest(payload)

	_, span := tracing.DefaultTracer().Start(c.ctx, "datasource.elasticsearch.queryData.executeMultisearch", trace.WithAttributes(
		attribute.String("url", c.ds.URL),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	start := time.Now()
	res, err := req.Do(c.ctx, c.ds.ESClient)
	if err != nil {
		status := "error"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		lp := []any{"error", err, "status", status, "duration", time.Since(start), "stage", StageDatabaseRequest}
		sourceErr := backend.ErrorWithSource{}
		if errors.As(err, &sourceErr) {
			lp = append(lp, "statusSource", sourceErr.ErrorSource())
		}
		if res != nil {
			lp = append(lp, "statusCode", res.StatusCode)
		}
		c.logger.Error("Error received from Elasticsearch", lp...)
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			c.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	c.logger.Info("Response received from Elasticsearch", "status", "ok", "statusCode", res.StatusCode, "contentLength", res.Header.Get("Content-Length"), "duration", time.Since(start), "stage", StageDatabaseRequest)

	_, resSpan := tracing.DefaultTracer().Start(c.ctx, "datasource.elasticsearch.queryData.executeMultisearch.decodeResponse")
	defer func() {
		if err != nil {
			resSpan.RecordError(err)
			resSpan.SetStatus(codes.Error, err.Error())
		}
		resSpan.End()
	}()

	improvedParsingEnabled := isFeatureEnabled(c.ctx, "elasticsearchImprovedParsing")
	msr, err := c.parser.parseMultiSearchResponse(res.Body, improvedParsingEnabled)
	if err != nil {
		return nil, err
	}

	msr.Status = res.StatusCode

	return msr, nil
}

// buildMsearchRequest constructs the typed esapi request with our serverless /
// frozen-index options applied. Pulling this out keeps the request shape easy
// to assert in unit tests.
func (c *baseClientImpl) buildMsearchRequest(payload []byte) esapi.MsearchRequest {
	req := esapi.MsearchRequest{
		Body: bytes.NewReader(payload),
	}

	// Serverless clusters don't support max_concurrent_shard_requests, so skip
	// the query param on that flavor.
	if !c.ds.ClusterInfo.IsServerless() && c.ds.MaxConcurrentShardRequests > 0 {
		mcsr := int(c.ds.MaxConcurrentShardRequests)
		req.MaxConcurrentShardRequests = &mcsr
	}

	// IncludeFrozen → ignore_throttled=false (i.e. don't throttle frozen indices).
	if c.ds.IncludeFrozen {
		ignoreThrottled := false
		req.IgnoreThrottled = &ignoreThrottled
	}

	return req
}

func (c *baseClientImpl) createMultiSearchRequests(searchRequests []*SearchRequest) ([]*multiRequest, error) {
	multiRequests := []*multiRequest{}

	for _, searchReq := range searchRequests {
		indices, err := c.indexPattern.GetIndices(searchReq.TimeRange)
		if err != nil {
			err := fmt.Errorf("failed to get indices from index pattern. %s", err)
			return nil, backend.DownstreamError(err)
		}
		mr := multiRequest{
			header: map[string]any{
				"search_type":        "query_then_fetch",
				"ignore_unavailable": true,
				"index":              strings.Join(indices, ","),
			},
			body:     searchReq,
			interval: searchReq.Interval,
		}

		multiRequests = append(multiRequests, &mr)
	}

	return multiRequests, nil
}

func (c *baseClientImpl) MultiSearch() *MultiSearchRequestBuilder {
	return NewMultiSearchRequestBuilder()
}

func (c *baseClientImpl) ExecuteEsql(query string) (*EsqlResponse, error) {
	var err error

	esqlRequest := EsqlRequest{
		Query: query,
	}

	payload, err := json.Marshal(esqlRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ES|QL request: %w", err)
	}

	_, span := tracing.DefaultTracer().Start(c.ctx, "datasource.elasticsearch.queryData.executeEsql", trace.WithAttributes(
		attribute.String("url", c.ds.URL),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	req := esapi.EsqlQueryRequest{Body: bytes.NewReader(payload)}

	start := time.Now()
	res, err := req.Do(c.ctx, c.ds.ESClient)
	if err != nil {
		status := "error"
		if errors.Is(err, context.Canceled) {
			status = "cancelled"
		}
		lp := []any{"error", err, "status", status, "duration", time.Since(start), "stage", StageDatabaseRequest}
		sourceErr := backend.ErrorWithSource{}
		if errors.As(err, &sourceErr) {
			lp = append(lp, "statusSource", sourceErr.ErrorSource())
		}
		if res != nil {
			lp = append(lp, "statusCode", res.StatusCode)
		}
		c.logger.Error("Error received from Elasticsearch ES|QL endpoint", lp...)
		return nil, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			c.logger.Warn("Failed to close response body", "error", err)
		}
	}()

	c.logger.Info("Response received from Elasticsearch ES|QL endpoint", "status", "ok", "statusCode", res.StatusCode, "contentLength", res.Header.Get("Content-Length"), "duration", time.Since(start), "stage", StageDatabaseRequest)

	// Check for error status codes
	if res.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, backend.DownstreamError(fmt.Errorf("ES|QL query failed with status %d: %s", res.StatusCode, string(bodyBytes)))
	}

	var esqlResponse EsqlResponse
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&esqlResponse); err != nil {
		return nil, backend.DownstreamError(fmt.Errorf("failed to decode ES|QL response: %w", err))
	}

	return &esqlResponse, nil
}

func isFeatureEnabled(ctx context.Context, feature string) bool {
	return backend.GrafanaConfigFromContext(ctx).FeatureToggles().IsEnabled(feature)
}

// StreamMultiSearchResponse processes the JSON response in a streaming fashion
// This is a public wrapper for backward compatibility
func StreamMultiSearchResponse(body io.Reader, msr *MultiSearchResponse) error {
	parser := newResponseParser(log.NewNullLogger())
	return parser.streamMultiSearchResponse(body, msr)
}
