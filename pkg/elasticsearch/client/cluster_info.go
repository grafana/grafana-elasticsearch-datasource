package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type VersionInfo struct {
	Number       string `json:"number"`
	BuildFlavor  string `json:"build_flavor"`
	Distribution string `json:"distribution"`
}

// ClusterInfo represents Elasticsearch cluster information returned from the root endpoint.
// It is used to determine cluster capabilities and configuration like whether the cluster is serverless.
type ClusterInfo struct {
	Version VersionInfo `json:"version"`
	Tagline string      `json:"tagline"`
	// Product holds the product identification response header sent by
	// Elasticsearch 7.14 and later. It is not part of the JSON payload.
	Product string `json:"-"`
}

const (
	BuildFlavorServerless = "serverless"

	// DistributionElasticsearch identifies a genuine Elasticsearch cluster.
	DistributionElasticsearch = "elasticsearch"
	// DistributionElasticsearchServerless identifies an Elastic serverless cluster.
	DistributionElasticsearchServerless = "elasticsearch_serverless"
	// DistributionUnknown is reported when no detection signal was available,
	// for example when the root endpoint is not accessible to the configured
	// credentials.
	DistributionUnknown = "unknown"
	// DistributionTagline is the distribution reported when only the tagline
	// identifies the cluster.
	DistributionTagline = taglineDistribution

	productHeaderName          = "X-Elastic-Product"
	productHeaderElasticsearch = "Elasticsearch"
	taglineElasticsearch       = "You Know, for Search"
	// taglineDistribution is the distribution reported when only the tagline
	// identifies the cluster, for example when a compatibility override hides
	// the other version payload signals.
	taglineDistribution = "opensearch"
)

// GetClusterInfo fetches cluster information from the Elasticsearch root endpoint.
// It returns the cluster build flavor which is used to determine if the cluster is serverless.
func GetClusterInfo(httpCli *http.Client, url string) (clusterInfo ClusterInfo, err error) {
	resp, err := httpCli.Get(url)
	if err != nil {
		return ClusterInfo{}, fmt.Errorf("error getting ES cluster info: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ClusterInfo{}, fmt.Errorf("unexpected status code %d getting ES cluster info", resp.StatusCode)
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("error closing response body: %w", closeErr)
		}
	}()

	err = json.NewDecoder(resp.Body).Decode(&clusterInfo)
	if err != nil {
		return ClusterInfo{}, fmt.Errorf("error decoding ES cluster info: %w", err)
	}

	clusterInfo.Product = resp.Header.Get(productHeaderName)

	return clusterInfo, nil
}

func (ci ClusterInfo) IsServerless() bool {
	return ci.Version.BuildFlavor == BuildFlavorServerless
}

// Distribution classifies the backend serving the root endpoint. Detection
// signals are checked from strongest to weakest: the product response header,
// the distribution reported in the version payload, and finally the tagline.
// The version number alone is never trusted, because compatibility overrides
// in other distributions can report a spoofed Elasticsearch version.
func (ci ClusterInfo) Distribution() string {
	switch {
	case ci.IsServerless():
		return DistributionElasticsearchServerless
	case ci.Product == productHeaderElasticsearch:
		return DistributionElasticsearch
	case ci.Version.Distribution != "":
		return strings.ToLower(ci.Version.Distribution)
	case strings.Contains(strings.ToLower(ci.Tagline), taglineDistribution):
		return taglineDistribution
	case ci.Tagline == taglineElasticsearch:
		return DistributionElasticsearch
	default:
		return DistributionUnknown
	}
}

// VersionMajor returns the major component of the reported version number.
func (ci ClusterInfo) VersionMajor() string {
	major, _, _ := strings.Cut(ci.Version.Number, ".")
	if major == "" {
		return DistributionUnknown
	}
	return major
}
