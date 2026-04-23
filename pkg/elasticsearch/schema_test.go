package elasticsearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	schemas "github.com/grafana/schemads"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func TestFallbackTableParams(t *testing.T) {
	p := fallbackTableParams()
	require.Len(t, p, 1)
	require.Equal(t, tableParamIndex, p[0].Name)
	require.True(t, p[0].Root)
	require.True(t, p[0].Required)
}

func TestDefaultSchemaSettings(t *testing.T) {
	s := defaultSchemaSettings()
	require.Equal(t, defaultSchemaMaxIndices, s.MaxIndices)
	require.False(t, s.IncludeHidden)
}

func TestSchemaProvider_resolveIndexForColumns(t *testing.T) {
	p := NewSchemaProvider(&DataSource{
		info: &es.DatasourceInfo{},
	})

	t.Run("returns table name when not the fallback table", func(t *testing.T) {
		idx, err := p.resolveIndexForColumns("my-index", nil)
		require.NoError(t, err)
		require.Equal(t, "my-index", idx)
	})

	t.Run("errors when fallback table is used without index parameter", func(t *testing.T) {
		_, err := p.resolveIndexForColumns(fallbackTableName, nil)
		require.Error(t, err)
	})

	t.Run("returns index parameter value when fallback table is used", func(t *testing.T) {
		idx, err := p.resolveIndexForColumns(fallbackTableName, map[string]string{tableParamIndex: "real-index"})
		require.NoError(t, err)
		require.Equal(t, "real-index", idx)
	})
}

func TestSchemaProvider_TableParameterValues_nonFallback(t *testing.T) {
	p := NewSchemaProvider(&DataSource{info: &es.DatasourceInfo{}})
	resp, err := p.TableParameterValues(context.Background(), &schemas.TableParameterValuesRequest{
		Table:          "other",
		TableParameter: tableParamIndex,
	})
	require.NoError(t, err)
	require.Empty(t, resp.TableParameterValues)
}

func TestSchemaProvider_resolveIndexForColumns_wildcard(t *testing.T) {
	p := NewSchemaProvider(&DataSource{info: &es.DatasourceInfo{}})
	idx, err := p.resolveIndexForColumns("logs-*", nil)
	require.NoError(t, err)
	require.Equal(t, "logs-*", idx)
}

func TestDefaultSchemaSettings_Timeouts(t *testing.T) {
	s := defaultSchemaSettings()
	require.NotZero(t, s.IndicesTimeout)
	require.NotZero(t, s.FieldCapsTimeout)
}

func TestFilterAndSortIndices(t *testing.T) {
	in := []string{".kibana", "logs-prod", "logs-staging", "metrics", ""}
	s := defaultSchemaSettings()
	names := filterAndSortIndices(in, &s)
	require.Equal(t, []string{"logs-prod", "logs-staging", "metrics"}, names)
}

func TestFilterAndSortIndices_IncludeHidden(t *testing.T) {
	in := []string{".kibana", "logs"}
	s := defaultSchemaSettings()
	s.IncludeHidden = true
	names := filterAndSortIndices(in, &s)
	require.Equal(t, []string{".kibana", "logs"}, names)
}

func TestFilterAndSortIndices_Dedupes(t *testing.T) {
	in := []string{"logs", "logs", " logs ", "metrics"}
	s := defaultSchemaSettings()
	names := filterAndSortIndices(in, &s)
	require.Equal(t, []string{"logs", "metrics"}, names)
}

func TestFilterAndSortIndices_RespectsMaxIndices(t *testing.T) {
	in := []string{"a", "b", "c", "d"}
	s := defaultSchemaSettings()
	s.MaxIndices = 2
	names := filterAndSortIndices(in, &s)
	require.Equal(t, []string{"a", "b"}, names)
}

func TestParseResolveResponse_CombinesAndSkipsBackingIndices(t *testing.T) {
	const body = `{
  "indices": [
    {"name": "metrics-2026.04.23"},
    {"name": ".ds-logs-prod-2026.04.23-000001", "data_stream": "logs-prod"}
  ],
  "aliases": [
    {"name": "metrics-current"}
  ],
  "data_streams": [
    {"name": "logs-prod"}
  ]
}`
	names, err := parseResolveResponse([]byte(body))
	require.NoError(t, err)
	// Backing index for the "logs-prod" data stream is skipped; the data
	// stream itself surfaces as a user-facing name. Aliases and the standalone
	// concrete index also come through.
	require.ElementsMatch(t, []string{"metrics-2026.04.23", "metrics-current", "logs-prod"}, names)
}

func TestParseResolveResponse_EmptyArrays(t *testing.T) {
	const body = `{"indices": [], "aliases": [], "data_streams": []}`
	names, err := parseResolveResponse([]byte(body))
	require.NoError(t, err)
	require.Empty(t, names)
}

func TestParseResolveResponse_InvalidJSON(t *testing.T) {
	_, err := parseResolveResponse([]byte("not-json"))
	require.Error(t, err)
}

func TestListAllIndexNames_PrefersResolveOverCat(t *testing.T) {
	var resolveCalls, catCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_resolve/index/*":
			atomic.AddInt32(&resolveCalls, 1)
			_, _ = w.Write([]byte(`{
              "indices": [{"name": "metrics"}],
              "aliases": [{"name": "metrics-current"}],
              "data_streams": [{"name": "logs-prod"}]
            }`))
		case r.URL.Path == "/_cat/indices":
			atomic.AddInt32(&catCalls, 1)
			_, _ = w.Write([]byte(`[{"index": "should-not-be-returned"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := defaultSchemaSettings()
	names, err := listAllIndexNames(context.Background(), &es.DatasourceInfo{
		URL:        srv.URL,
		HTTPClient: srv.Client(),
	}, &s)
	require.NoError(t, err)
	require.Equal(t, []string{"logs-prod", "metrics", "metrics-current"}, names)
	require.Equal(t, int32(1), atomic.LoadInt32(&resolveCalls))
	require.Equal(t, int32(0), atomic.LoadInt32(&catCalls), "cat must not be called when resolve succeeds")
}

func TestListAllIndexNames_FallsBackToCatWhenResolveFails(t *testing.T) {
	var resolveCalls, catCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/_resolve/index/*":
			atomic.AddInt32(&resolveCalls, 1)
			http.Error(w, "no such handler", http.StatusNotFound) // simulate ES < 7.9
		case r.URL.Path == "/_cat/indices":
			atomic.AddInt32(&catCalls, 1)
			_, _ = w.Write([]byte(`[{"index": "logs"}, {"index": "metrics"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	s := defaultSchemaSettings()
	names, err := listAllIndexNames(context.Background(), &es.DatasourceInfo{
		URL:        srv.URL,
		HTTPClient: srv.Client(),
	}, &s)
	require.NoError(t, err)
	require.Equal(t, []string{"logs", "metrics"}, names)
	require.Equal(t, int32(1), atomic.LoadInt32(&resolveCalls))
	require.Equal(t, int32(1), atomic.LoadInt32(&catCalls))
}

func TestListAllIndexNames_ErrorIncludesBothFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer srv.Close()

	s := defaultSchemaSettings()
	_, err := listAllIndexNames(context.Background(), &es.DatasourceInfo{
		URL:        srv.URL,
		HTTPClient: srv.Client(),
	}, &s)
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve failed")
	require.Contains(t, err.Error(), "cat fallback failed")
}

func TestIsForbiddenOrUnauthorized(t *testing.T) {
	require.True(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 403: forbidden")))
	require.True(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 401: unauthorized")))
	require.False(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 500: internal")))
}

func TestSchemaProvider_cacheKey(t *testing.T) {
	p := NewSchemaProvider(&DataSource{info: &es.DatasourceInfo{ID: 42}})

	t.Run("uses namespace, datasource ID, user login and sub", func(t *testing.T) {
		ctx := backend.WithPluginContext(context.Background(), backend.PluginContext{
			Namespace: "stacks-7",
			User:      &backend.User{Login: "alice"},
		})
		require.Equal(t, "stacks-7:42:indices", p.cacheKey(ctx, "indices"))
		require.Equal(t, "stacks-7:42:cols:logs-*", p.cacheKey(ctx, "cols:logs-*"))
	})

	t.Run("falls back to underscores when namespace and user are missing", func(t *testing.T) {
		ctx := backend.WithPluginContext(context.Background(), backend.PluginContext{})
		require.Equal(t, "_:42:indices", p.cacheKey(ctx, "indices"))
	})

	t.Run("different namespaces produce different keys", func(t *testing.T) {
		ctxA := backend.WithPluginContext(context.Background(), backend.PluginContext{Namespace: "a"})
		ctxB := backend.WithPluginContext(context.Background(), backend.PluginContext{Namespace: "b"})
		require.NotEqual(t, p.cacheKey(ctxA, "indices"), p.cacheKey(ctxB, "indices"))
	})
}

func TestSchemaProvider_cachedFieldCapsColumns_singleflightDedupes(t *testing.T) {
	// Concurrent callers requesting the same index should collapse into a
	// single fetch, with all callers receiving the same result.
	var calls int32
	fetch := func() ([]schemas.Column, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return []schemas.Column{{Name: "f", Type: schemas.ColumnTypeString}}, nil
	}

	p := NewSchemaProvider(&DataSource{info: &es.DatasourceInfo{ID: 1}})
	ctx := backend.WithPluginContext(context.Background(), backend.PluginContext{Namespace: "ns"})

	const n = 50
	var wg sync.WaitGroup
	results := make([][]schemas.Column, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			key := p.cacheKey(ctx, "cols:my-index")
			p.mu.Lock()
			if e, ok := p.colCache[key]; ok && time.Since(e.at) < schemaIndicesCacheTTL && len(e.cols) > 0 {
				p.mu.Unlock()
				results[i] = e.cols
				return
			}
			p.mu.Unlock()

			v, err, _ := p.sf.Do(key, func() (interface{}, error) {
				cols, err := fetch()
				if err != nil {
					return nil, err
				}
				p.mu.Lock()
				p.colCache[key] = columnCacheEntry{at: time.Now(), cols: cols}
				p.mu.Unlock()
				return cols, nil
			})
			require.NoError(t, err)
			results[i] = v.([]schemas.Column)
		}(i)
	}
	wg.Wait()

	require.Equal(t, int32(1), atomic.LoadInt32(&calls), "singleflight should collapse concurrent fetches into one call")
	for i := 0; i < n; i++ {
		require.Equal(t, []schemas.Column{{Name: "f", Type: schemas.ColumnTypeString}}, results[i])
	}
}

func TestSchemaProvider_cachedFieldCapsColumns_differentKeysRunInParallel(t *testing.T) {
	// Different cache keys (different indices) must NOT be
	// collapsed by singleflight - they should fetch independently.
	var calls int32
	fetch := func() {
		atomic.AddInt32(&calls, 1)
	}

	p := NewSchemaProvider(&DataSource{info: &es.DatasourceInfo{ID: 1}})

	keys := []string{
		p.cacheKey(backend.WithPluginContext(context.Background(),
			backend.PluginContext{Namespace: "ns"}), "cols:logs"),
		p.cacheKey(backend.WithPluginContext(context.Background(),
			backend.PluginContext{Namespace: "ns"}), "cols:metrics"),
	}
	require.Len(t, keys, 2)
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			require.NotEqual(t, keys[i], keys[j])
		}
	}

	var wg sync.WaitGroup
	for _, k := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			_, _, _ = p.sf.Do(k, func() (interface{}, error) {
				fetch()
				return nil, nil
			})
		}(k)
	}
	wg.Wait()

	require.Equal(t, int32(2), atomic.LoadInt32(&calls), "distinct keys must not be deduped")
}
