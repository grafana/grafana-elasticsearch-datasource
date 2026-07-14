package client

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetClusterInfo_LiveCluster verifies distribution detection against a real
// cluster, complementing the fixture-based unit tests. It is skipped unless
// CLUSTER_INFO_TEST_URL points at a running cluster. Set
// CLUSTER_INFO_TEST_DISTRIBUTION to the expected Distribution() result and
// optionally CLUSTER_INFO_TEST_VERSION_MAJOR to the expected major version.
//
// Example against the docker-compose Elasticsearch service:
//
//	CLUSTER_INFO_TEST_URL=http://localhost:9200 \
//	CLUSTER_INFO_TEST_DISTRIBUTION=elasticsearch \
//	go test ./pkg/elasticsearch/client/ -run TestGetClusterInfo_LiveCluster -v
func TestGetClusterInfo_LiveCluster(t *testing.T) {
	url := os.Getenv("CLUSTER_INFO_TEST_URL")
	if url == "" {
		t.Skip("CLUSTER_INFO_TEST_URL not set, skipping live cluster test")
	}

	expectedDistribution := os.Getenv("CLUSTER_INFO_TEST_DISTRIBUTION")
	require.NotEmpty(t, expectedDistribution, "CLUSTER_INFO_TEST_DISTRIBUTION must be set when CLUSTER_INFO_TEST_URL is set")

	clusterInfo, err := GetClusterInfo(&http.Client{Timeout: 30 * time.Second}, url)
	require.NoError(t, err)

	assert.Equal(t, expectedDistribution, clusterInfo.Distribution())

	if expectedMajor := os.Getenv("CLUSTER_INFO_TEST_VERSION_MAJOR"); expectedMajor != "" {
		assert.Equal(t, expectedMajor, clusterInfo.VersionMajor())
	}

	t.Logf("detected distribution=%s version=%s major=%s product=%q tagline=%q",
		clusterInfo.Distribution(), clusterInfo.Version.Number, clusterInfo.VersionMajor(),
		clusterInfo.Product, clusterInfo.Tagline)
}
