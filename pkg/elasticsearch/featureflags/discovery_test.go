package featureflags

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// recordingServer serves one OFREP answer and records the requests it receives.
type recordingServer struct {
	*httptest.Server
	requests atomic.Int64
	lastPath atomic.Value
	lastBody atomic.Value
}

func newRecordingServer(t *testing.T, status int, body map[string]any) *recordingServer {
	t.Helper()
	s := &recordingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.lastPath.Store(r.URL.Path)
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			s.lastBody.Store(req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// discoveryContext returns a request context whose Grafana configuration
// advertises an OpenFeature endpoint, as a host would.
func discoveryContext(ctx context.Context, endpoint, providerType, namespace string, extra map[string]string) context.Context {
	cfg := map[string]string{}
	if endpoint != "" {
		cfg[config.OpenFeatureProviderURL] = endpoint
	}
	if providerType != "" {
		cfg[config.OpenFeatureProviderType] = providerType
	}
	for k, v := range extra {
		cfg[k] = v
	}
	ctx = config.WithGrafanaConfig(ctx, config.NewGrafanaCfg(cfg))
	return backend.WithPluginContext(ctx, backend.PluginContext{Namespace: namespace})
}

func TestAdvertisedEndpointReplacesFallback(t *testing.T) {
	fallback := newRecordingServer(t, http.StatusOK, disabledResponse(LogsDataplane))
	advertised := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
	ctx := discoveryContext(context.Background(), advertised.URL, "ofrep", "stacks-1", nil)

	require.True(t, NewClient(fallback.URL).IsEnabled(ctx, LogsDataplane))
	require.Equal(t, int64(1), advertised.requests.Load())
	require.Equal(t, int64(0), fallback.requests.Load())
}

func TestUnusableAdvertisementUsesFallback(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		extra        map[string]string
	}{
		{name: "features-service endpoint", providerType: "features-service"},
		{name: "static endpoint", providerType: "static"},
		{name: "URL without a provider type", providerType: ""},
		{name: "malformed cache TTL", providerType: "ofrep", extra: map[string]string{config.OpenFeatureCacheTTL: "soon"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fallback := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
			advertised := newRecordingServer(t, http.StatusOK, disabledResponse(LogsDataplane))
			ctx := discoveryContext(context.Background(), advertised.URL, tt.providerType, "stacks-1", tt.extra)

			require.True(t, NewClient(fallback.URL).IsEnabled(ctx, LogsDataplane))
			require.Equal(t, int64(1), fallback.requests.Load())
			require.Equal(t, int64(0), advertised.requests.Load())
		})
	}
}

func TestAdvertisedEndpointKeepsSubPath(t *testing.T) {
	advertised := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
	ctx := discoveryContext(context.Background(), advertised.URL+"/flags/", "ofrep", "stacks-1", nil)

	require.True(t, NewClient("http://fallback.invalid").IsEnabled(ctx, LogsDataplane))
	require.Equal(t, "/flags/ofrep/v1/evaluate/flags/"+LogsDataplane, advertised.lastPath.Load())
}

func TestAdvertisedEvaluationContext(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		attrs     string
		tenantID  string
		want      map[string]any
	}{
		{
			name:      "request namespace is the targeting key and host attributes are merged",
			namespace: "stacks-42",
			attrs:     `{"stackId":"42","slug":"mystack","env":"test"}`,
			want:      map[string]any{"targetingKey": "stacks-42", "namespace": "stacks-42", "stackId": "42", "slug": "mystack", "env": "test"},
		},
		{
			name:  "host namespace is the fallback targeting key",
			attrs: `{"namespace":"stacks-7"}`,
			want:  map[string]any{"targetingKey": "stacks-7", "namespace": "stacks-7"},
		},
		{
			name:      "host attributes win over plugin attributes",
			namespace: "stacks-1",
			attrs:     `{"namespace":"stacks-9"}`,
			want:      map[string]any{"targetingKey": "stacks-1", "namespace": "stacks-9"},
		},
		{
			name:      "tenant metadata adds nothing when the host advertises an endpoint",
			namespace: "stacks-5",
			tenantID:  "99",
			want:      map[string]any{"targetingKey": "stacks-5", "namespace": "stacks-5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advertised := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
			extra := map[string]string{}
			if tt.attrs != "" {
				extra[config.OpenFeatureContext] = tt.attrs
			}
			ctx := context.Background()
			if tt.tenantID != "" {
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(tenantIDMetadataKey, tt.tenantID))
			}
			NewClient("http://fallback.invalid").IsEnabled(discoveryContext(ctx, advertised.URL, "ofrep", tt.namespace, extra), LogsDataplane)

			body, ok := advertised.lastBody.Load().(map[string]any)
			require.True(t, ok, "the advertised endpoint must receive an OFREP request body")
			require.Equal(t, tt.want, body["context"])
		})
	}
}

func TestAdvertisedEndpointsAreCachedSeparately(t *testing.T) {
	first := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
	second := newRecordingServer(t, http.StatusOK, disabledResponse(LogsDataplane))
	c := NewClient("http://fallback.invalid")

	require.True(t, c.IsEnabled(discoveryContext(context.Background(), first.URL, "ofrep", "stacks-1", nil), LogsDataplane))
	require.False(t, c.IsEnabled(discoveryContext(context.Background(), second.URL, "ofrep", "stacks-1", nil), LogsDataplane))
	require.True(t, c.IsEnabled(discoveryContext(context.Background(), first.URL, "ofrep", "stacks-1", nil), LogsDataplane))

	require.Equal(t, int64(1), first.requests.Load())
	require.Equal(t, int64(1), second.requests.Load())
}

func TestAdvertisedCacheTTL(t *testing.T) {
	require.Equal(t, defaultCacheTTL, cacheTTL(config.OpenFeatureConfig{}))
	require.Equal(t, 30*time.Second, cacheTTL(config.OpenFeatureConfig{CacheTTL: 30 * time.Second}))

	advertised := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
	ctx := discoveryContext(context.Background(), advertised.URL, "ofrep", "stacks-1", map[string]string{config.OpenFeatureCacheTTL: "1"})
	c := NewClient("http://fallback.invalid")

	c.IsEnabled(ctx, LogsDataplane)
	require.Eventually(t, func() bool {
		c.IsEnabled(ctx, LogsDataplane)
		return advertised.requests.Load() == 2
	}, 3*time.Second, 50*time.Millisecond)
}

func TestFailedRefreshKeepsPreviousAnswer(t *testing.T) {
	var requests atomic.Int64
	var failing atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"errorDetails": "boom"})
			return
		}
		_ = json.NewEncoder(w).Encode(enabledResponse(LogsDataplane))
	}))
	t.Cleanup(srv.Close)
	ctx := discoveryContext(context.Background(), srv.URL, "ofrep", "stacks-1", map[string]string{config.OpenFeatureCacheTTL: "1"})
	c := NewClient("http://fallback.invalid")

	require.True(t, c.IsEnabled(ctx, LogsDataplane))
	failing.Store(true)
	require.Eventually(t, func() bool {
		c.IsEnabled(ctx, LogsDataplane)
		return requests.Load() == 2
	}, 3*time.Second, 50*time.Millisecond)
	require.True(t, c.IsEnabled(ctx, LogsDataplane), "a failed refresh must not replace the last answer with off")
}

func TestEvaluateReusesEntryCachedByAnotherCaller(t *testing.T) {
	advertised := newRecordingServer(t, http.StatusOK, enabledResponse(LogsDataplane))
	ctx := discoveryContext(context.Background(), advertised.URL, "ofrep", "stacks-1", nil)
	c := NewClient("http://fallback.invalid")
	require.True(t, c.IsEnabled(ctx, LogsDataplane))

	// A caller that read a miss before the first evaluation finished reaches evaluate with a fresh entry in the cache.
	tgt := c.target(ctx)
	require.True(t, c.evaluate(ctx, cacheKey(LogsDataplane, tgt), LogsDataplane, tgt).value)
	require.Equal(t, int64(1), advertised.requests.Load())
}
