package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
)

type VersionInfo struct {
	BuildFlavor string `json:"build_flavor"`
}

// ClusterInfo represents Elasticsearch cluster information returned from the root endpoint.
// It is used to determine cluster capabilities and configuration like whether the cluster is serverless.
type ClusterInfo struct {
	Version VersionInfo `json:"version"`
}

const (
	BuildFlavorServerless = "serverless"
)

// GetClusterInfo fetches cluster information from the Elasticsearch root endpoint.
// It returns the cluster build flavor which is used to determine if the cluster is serverless.
func GetClusterInfo(ctx context.Context, esClient *elasticsearch.Client) (clusterInfo ClusterInfo, err error) {
	if esClient == nil {
		return ClusterInfo{}, fmt.Errorf("elasticsearch client is required to get cluster info")
	}

	req := esapi.InfoRequest{}
	res, err := req.Do(ctx, esClient)
	if err != nil {
		return ClusterInfo{}, fmt.Errorf("error getting ES cluster info: %w", err)
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("error closing response body: %w", closeErr)
		}
	}()

	if res.StatusCode != http.StatusOK {
		return ClusterInfo{}, fmt.Errorf("unexpected status code %d getting ES cluster info", res.StatusCode)
	}

	if err = json.NewDecoder(res.Body).Decode(&clusterInfo); err != nil {
		return ClusterInfo{}, fmt.Errorf("error decoding ES cluster info: %w", err)
	}

	return clusterInfo, nil
}

func (ci ClusterInfo) IsServerless() bool {
	return ci.Version.BuildFlavor == BuildFlavorServerless
}
