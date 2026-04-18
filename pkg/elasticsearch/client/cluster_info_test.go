package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetClusterInfo(t *testing.T) {
	t.Run("Should successfully get cluster info", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			_, err := rw.Write([]byte(`{
				"name": "test-cluster",
				"cluster_name": "elasticsearch",
				"cluster_uuid": "abc123",
				"version": {
					"number": "8.0.0",
					"build_flavor": "default",
					"build_type": "tar",
					"build_hash": "abc123",
					"build_date": "2023-01-01T00:00:00.000Z",
					"build_snapshot": false,
					"lucene_version": "9.0.0"
				}
			}`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.NoError(t, err)
		require.NotNil(t, clusterInfo)
		assert.Equal(t, "default", clusterInfo.Version.BuildFlavor)
	})

	t.Run("Should successfully get serverless cluster info", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			_, err := rw.Write([]byte(`{
				"name": "serverless-cluster",
				"cluster_name": "elasticsearch",
				"cluster_uuid": "def456",
				"version": {
					"number": "8.11.0",
					"build_flavor": "serverless",
					"build_type": "docker",
					"build_hash": "def456",
					"build_date": "2023-11-01T00:00:00.000Z",
					"build_snapshot": false,
					"lucene_version": "9.8.0"
				}
			}`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.NoError(t, err)
		require.NotNil(t, clusterInfo)
		assert.Equal(t, "serverless", clusterInfo.Version.BuildFlavor)
		assert.True(t, clusterInfo.IsServerless())
	})

	t.Run("Should return error when HTTP request fails", func(t *testing.T) {
		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, http.DefaultClient, "http://invalid-url-that-does-not-exist.local:9999"))

		require.Error(t, err)
		require.Equal(t, ClusterInfo{}, clusterInfo)
		assert.Contains(t, err.Error(), "error getting ES cluster info")
	})

	t.Run("Should return error when response body is invalid JSON", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			_, err := rw.Write([]byte(`{"invalid json`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.Error(t, err)
		require.Equal(t, ClusterInfo{}, clusterInfo)
		assert.Contains(t, err.Error(), "error decoding ES cluster info")
	})

	t.Run("Should handle empty version object", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			_, err := rw.Write([]byte(`{
				"name": "test-cluster",
				"version": {}
			}`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.NoError(t, err)
		require.Equal(t, ClusterInfo{Product: "Elasticsearch"}, clusterInfo)
		assert.Equal(t, "", clusterInfo.Version.BuildFlavor)
		assert.False(t, clusterInfo.IsServerless())
	})

	t.Run("Should handle HTTP error status codes", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.WriteHeader(http.StatusUnauthorized)
			_, err := rw.Write([]byte(`{"error": "Unauthorized"}`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.Error(t, err)
		require.Equal(t, ClusterInfo{}, clusterInfo)
		assert.Contains(t, err.Error(), "unexpected status code 401 getting ES cluster info")
	})

	t.Run("Should return error when elasticsearch client is nil", func(t *testing.T) {
		clusterInfo, err := GetClusterInfo(context.Background(), nil)

		require.Error(t, err)
		require.Equal(t, ClusterInfo{}, clusterInfo)
		assert.Contains(t, err.Error(), "elasticsearch client is required")
	})
}

func TestGetClusterInfo_DetectionFields(t *testing.T) {
	t.Run("Should capture version number, tagline and product header", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/json")
			rw.Header().Set("X-Elastic-Product", "Elasticsearch")
			_, err := rw.Write([]byte(`{
				"name": "test-node",
				"cluster_name": "elasticsearch",
				"version": {
					"number": "9.1.0",
					"build_flavor": "default"
				},
				"tagline": "You Know, for Search"
			}`))
			require.NoError(t, err)
		}))

		t.Cleanup(func() {
			ts.Close()
		})

		clusterInfo, err := GetClusterInfo(context.Background(), newTestESClient(t, ts.Client(), ts.URL))

		require.NoError(t, err)
		assert.Equal(t, "9.1.0", clusterInfo.Version.Number)
		assert.Equal(t, "You Know, for Search", clusterInfo.Tagline)
		assert.Equal(t, "Elasticsearch", clusterInfo.Product)
	})
}

func TestClusterInfo_Distribution(t *testing.T) {
	tests := []struct {
		name        string
		clusterInfo ClusterInfo
		expected    string
	}{
		{
			name: "serverless build flavor",
			clusterInfo: ClusterInfo{
				Product: "Elasticsearch",
				Version: VersionInfo{Number: "8.11.0", BuildFlavor: BuildFlavorServerless},
				Tagline: "You Know, for Search",
			},
			expected: DistributionElasticsearchServerless,
		},
		{
			name: "product header present",
			clusterInfo: ClusterInfo{
				Product: "Elasticsearch",
				Version: VersionInfo{Number: "9.1.0", BuildFlavor: "default"},
				Tagline: "You Know, for Search",
			},
			expected: DistributionElasticsearch,
		},
		{
			name: "reported distribution value used verbatim",
			clusterInfo: ClusterInfo{
				Version: VersionInfo{Number: "2.11.0", Distribution: "customdistro"},
				Tagline: "The CustomDistro Project",
			},
			expected: "customdistro",
		},
		{
			name: "reported distribution value is lowercased",
			clusterInfo: ClusterInfo{
				Version: VersionInfo{Number: "2.11.0", Distribution: "CustomDistro"},
			},
			expected: "customdistro",
		},
		{
			name: "spoofed version number falls back to tagline",
			clusterInfo: ClusterInfo{
				Version: VersionInfo{Number: "7.10.2"},
				Tagline: "The " + taglineDistribution + " Project",
			},
			expected: taglineDistribution,
		},
		{
			name: "tagline matching is case-insensitive",
			clusterInfo: ClusterInfo{
				Version: VersionInfo{Number: "7.10.2"},
				Tagline: "The " + strings.ToUpper(taglineDistribution) + " Project",
			},
			expected: taglineDistribution,
		},
		{
			name: "legacy version without product header falls back to tagline",
			clusterInfo: ClusterInfo{
				Version: VersionInfo{Number: "7.10.2", BuildFlavor: "default"},
				Tagline: "You Know, for Search",
			},
			expected: DistributionElasticsearch,
		},
		{
			name:        "no detection signals",
			clusterInfo: ClusterInfo{},
			expected:    DistributionUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.clusterInfo.Distribution())
		})
	}
}

func TestClusterInfo_VersionMajor(t *testing.T) {
	tests := []struct {
		name     string
		number   string
		expected string
	}{
		{name: "major.minor.patch", number: "8.19.4", expected: "8"},
		{name: "pre-release suffix", number: "9.0.0-SNAPSHOT", expected: "9"},
		{name: "empty version", number: "", expected: DistributionUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clusterInfo := ClusterInfo{Version: VersionInfo{Number: tc.number}}
			assert.Equal(t, tc.expected, clusterInfo.VersionMajor())
		})
	}
}

func TestClusterInfo_IsServerless(t *testing.T) {
	t.Run("Should return true when build_flavor is serverless", func(t *testing.T) {
		clusterInfo := ClusterInfo{
			Version: VersionInfo{
				BuildFlavor: BuildFlavorServerless,
			},
		}

		assert.True(t, clusterInfo.IsServerless())
	})

	t.Run("Should return false when build_flavor is default", func(t *testing.T) {
		clusterInfo := ClusterInfo{
			Version: VersionInfo{
				BuildFlavor: "default",
			},
		}

		assert.False(t, clusterInfo.IsServerless())
	})

	t.Run("Should return false when build_flavor is empty", func(t *testing.T) {
		clusterInfo := ClusterInfo{
			Version: VersionInfo{
				BuildFlavor: "",
			},
		}

		assert.False(t, clusterInfo.IsServerless())
	})

	t.Run("Should return false when build_flavor is unknown value", func(t *testing.T) {
		clusterInfo := ClusterInfo{
			Version: VersionInfo{
				BuildFlavor: "unknown",
			},
		}

		assert.False(t, clusterInfo.IsServerless())
	})

	t.Run("should return false when cluster info is empty", func(t *testing.T) {
		clusterInfo := ClusterInfo{}
		assert.False(t, clusterInfo.IsServerless())
	})
}
