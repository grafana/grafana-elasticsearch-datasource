package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/config"
)

const ErrorBodyMaxSize = 200

func (ds *DataSource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	logger := ds.logger.FromContext(ctx)

	healthStatusUrl, err := url.Parse(ds.info.URL)
	if err != nil {
		logger.Error("Failed to parse data source URL", "error", err)
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusUnknown,
			Message: "Failed to parse data source URL",
		}, nil
	}

	clusterInfo := ds.info.ClusterInfo
	if clusterInfo.IsEmpty() {
		// Cluster info detection failed when the instance was created, for
		// example because the root endpoint was unreachable at that point, so
		// the serverless classification may be wrong. Retry here so a
		// serverless cluster is not sent to _cluster/health, which it answers
		// with 410 Gone.
		refetched, err := es.GetClusterInfo(ctx, ds.info.HTTPClient, ds.info.URL)
		if err != nil {
			logger.Warn("Failed to get Elasticsearch cluster info during health check", "error", err)
		} else {
			clusterInfo = refetched
		}
	}

	// Serverless clusters do not support _cluster/health, so validate data access instead
	if clusterInfo.IsServerless() {
		return ds.checkServerlessHealth(ctx)
	}

	// check that ES is healthy
	healthStatusUrl.Path = path.Join(healthStatusUrl.Path, "_cluster/health")
	healthStatusUrl.RawQuery = "wait_for_status=yellow"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthStatusUrl.String(), nil)
	if err != nil {
		logger.Error("Failed to create request", "error", err, "url", healthStatusUrl.String())
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusUnknown,
			Message: "Failed to create request",
		}, nil
	}

	start := time.Now()
	logger.Debug("Sending healthcheck request to Elasticsearch", "url", healthStatusUrl.String())
	response, err := ds.info.HTTPClient.Do(request)

	if err != nil {
		logger.Error("Failed to connect to Elasticsearch", "error", err, "url", healthStatusUrl.String())
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Health check failed: Failed to connect to Elasticsearch",
		}, nil
	}

	defer func() {
		if err := response.Body.Close(); err != nil {
			logger.Warn("Failed to close response body", "error", err)
		}
	}()

	if response.StatusCode == http.StatusGone {
		// Serverless clusters answer unsupported endpoints such as
		// _cluster/health with 410 Gone. Reaching this point means the
		// cluster responded but serverless detection via the root endpoint
		// failed, so fall back to the serverless data-access check rather
		// than reporting the raw 410 to the user.
		logger.Debug("_cluster/health returned 410 Gone, treating cluster as serverless")
		return ds.checkServerlessHealth(ctx)
	}

	if response.StatusCode == http.StatusRequestTimeout {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Health check failed: Elasticsearch data source is not healthy. Request timed out",
		}, nil
	}

	if response.StatusCode >= 400 {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("Health check failed: Elasticsearch data source is not healthy. Status: %s", response.Status),
		}, nil
	}

	logger.Info("Response received from Elasticsearch", "statusCode", response.StatusCode, "status", "ok", "duration", time.Since(start))

	body, err := io.ReadAll(response.Body)
	if err != nil {
		logger.Error("Error reading response body bytes", "error", err)
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusUnknown,
			Message: "Health check failed: Failed to read response",
		}, nil
	}

	jsonData := map[string]any{}

	err = json.Unmarshal(body, &jsonData)
	if err != nil {
		truncatedBody := string(body)
		if len(truncatedBody) > ErrorBodyMaxSize {
			truncatedBody = truncatedBody[:ErrorBodyMaxSize] + "..."
		}
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusUnknown,
			Message: fmt.Sprintf("Health check failed: Failed to parse response from Elasticsearch. Response received: %s", truncatedBody),
		}, nil
	}

	if jsonData["status"] == "red" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Health check failed: Elasticsearch data source is not healthy",
		}, nil
	}

	successMessage := "Elasticsearch data source is healthy."
	indexWarningMessage := ""

	// validate index and time field
	cfg := config.GrafanaConfigFromContext(ctx)
	crossClusterSearchEnabled := cfg.FeatureToggles().IsEnabled("elasticsearchCrossClusterSearch")

	if crossClusterSearchEnabled {
		message, level := validateIndex(ctx, ds.info)
		if level == "warning" {
			indexWarningMessage = message
		}
		if level == "error" {
			return &backend.CheckHealthResult{
				Status:  backend.HealthStatusError,
				Message: message,
			}, nil
		}
	}

	if indexWarningMessage != "" {
		successMessage = fmt.Sprintf("%s Warning: %s", successMessage, indexWarningMessage)
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: successMessage,
	}, nil
}

// checkServerlessHealth reports the health of a serverless cluster, where
// _cluster/health is unavailable (it answers 410 Gone). _field_caps is
// supported on serverless, so index validation doubles as the connectivity
// and credentials probe.
func (ds *DataSource) checkServerlessHealth(ctx context.Context) (*backend.CheckHealthResult, error) {
	message, level := validateIndex(ctx, ds.info)
	if level == "error" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: message,
		}, nil
	}

	successMessage := "Elasticsearch Serverless data source is healthy."
	if level == "warning" {
		successMessage = fmt.Sprintf("%s Warning: %s", successMessage, message)
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: successMessage,
	}, nil
}

func validateIndex(ctx context.Context, ds *es.DatasourceInfo) (message string, level string) {
	// validate that the index exist and has date field
	ip, err := es.NewIndexPattern(ds.Interval, ds.Database)
	if err != nil {
		return fmt.Sprintf("Failed to get build index pattern: %s", err), "error"
	}

	indices, err := ip.GetIndices(backend.TimeRange{
		From: time.Now().UTC(),
		To:   time.Now().UTC(),
	})
	if err != nil {
		return fmt.Sprintf("Failed to get index pattern: %s", err), "error"
	}

	indexList := strings.Join(indices, ",")

	validateUrl := fmt.Sprintf("%s/%s/_field_caps?fields=%s", ds.URL, indexList, ds.ConfiguredFields.TimeField)
	if indexList == "" || strings.ReplaceAll(indexList, ",", "") == "" {
		validateUrl = fmt.Sprintf("%s/_field_caps?fields=%s", ds.URL, ds.ConfiguredFields.TimeField)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, validateUrl, nil)
	if err != nil {
		return fmt.Sprint("Failed to create request", "error", err, "url", validateUrl), "error"
	}
	response, err := ds.HTTPClient.Do(request)
	if err != nil {
		return fmt.Sprint("Failed to fetch field capabilities", "error", err, "url", validateUrl), "error"
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			backend.Logger.Warn("Failed to close response body", "error", err)
		}
	}()

	fieldCaps := map[string]any{}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "Could not read response body while checking time field", "error"
	}
	err = json.Unmarshal(body, &fieldCaps)
	if err != nil {
		return "Failed to unmarshal field capabilities response", "error"
	}
	if fieldCaps["error"] != nil {
		// Rejected credentials mean the data source cannot query at all, so
		// they are reported as unhealthy rather than as a warning.
		errorLevel := "warning"
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			errorLevel = "error"
		}
		errorMap, ok := fieldCaps["error"].(map[string]any)
		if !ok {
			return "Error validating index", errorLevel
		}
		errorMessage, ok := errorMap["reason"].(string)
		if !ok {
			return "Error validating index", errorLevel
		}
		return fmt.Sprintf("Error validating index: %s", errorMessage), errorLevel
	}

	fields, ok := fieldCaps["fields"].(map[string]any)
	if !ok {
		return "Failed to parse fields from response", "error"
	}
	if len(fields) == 0 {
		return fmt.Sprintf("Could not find field %s in index", ds.ConfiguredFields.TimeField), "warning"
	}

	timeFieldInfo, ok := fields[ds.ConfiguredFields.TimeField].(map[string]any)
	if !ok {
		return "Failed to parse time field info from response", "error"
	}

	dateTypeField, ok := timeFieldInfo["date"].(map[string]any)
	if !ok || dateTypeField == nil {
		return fmt.Sprintf("Could not find time field '%s' with type date in index", ds.ConfiguredFields.TimeField), "warning"
	}

	return "", ""
}
