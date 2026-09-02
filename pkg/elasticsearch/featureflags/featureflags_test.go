package featureflags

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

// ofrepHandler serves the single-flag OFREP evaluation endpoint, replying with
// the given status and body and counting requests.
func ofrepHandler(t *testing.T, status int, body map[string]any, requests *atomic.Int64) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(body))
	})
}

func enabledResponse(flag string) map[string]any {
	return map[string]any{
		"value":   true,
		"key":     flag,
		"reason":  "TARGETING_MATCH",
		"variant": "enabled",
	}
}

func disabledResponse(flag string) map[string]any {
	return map[string]any{
		"value":   false,
		"key":     flag,
		"reason":  "DEFAULT",
		"variant": "disabled",
	}
}

func TestClientIsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     map[string]any
		expected bool
	}{
		{
			name:     "enabled flag returns true",
			status:   http.StatusOK,
			body:     enabledResponse(LogsDataplane),
			expected: true,
		},
		{
			name:     "disabled flag returns false",
			status:   http.StatusOK,
			body:     disabledResponse(LogsDataplane),
			expected: false,
		},
		{
			name:     "undefined flag fails closed",
			status:   http.StatusNotFound,
			body:     map[string]any{"key": LogsDataplane, "errorCode": "FLAG_NOT_FOUND", "errorDetails": "flag not found"},
			expected: false,
		},
		{
			name:     "server error fails closed",
			status:   http.StatusInternalServerError,
			body:     map[string]any{"errorDetails": "boom"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(ofrepHandler(t, tt.status, tt.body, &requests))
			t.Cleanup(server.Close)

			client := NewClient(server.URL)
			require.Equal(t, tt.expected, client.IsEnabled(context.Background(), LogsDataplane))
		})
	}
}

func TestClientIsEnabledUnreachableFailsClosed(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	client := NewClient(server.URL)
	require.False(t, client.IsEnabled(context.Background(), LogsDataplane))
}

func TestClientCachesEvaluations(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   map[string]any
	}{
		{name: "successful evaluations are cached", status: http.StatusOK, body: enabledResponse(LogsDataplane)},
		{name: "failed evaluations are cached", status: http.StatusNotFound, body: map[string]any{"key": LogsDataplane, "errorCode": "FLAG_NOT_FOUND"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(ofrepHandler(t, tt.status, tt.body, &requests))
			t.Cleanup(server.Close)

			client := NewClient(server.URL)
			first := client.IsEnabled(context.Background(), LogsDataplane)
			second := client.IsEnabled(context.Background(), LogsDataplane)
			require.Equal(t, first, second)
			require.Equal(t, int64(1), requests.Load())
		})
	}
}

func TestClientCachesPerTenant(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(ofrepHandler(t, http.StatusOK, enabledResponse(LogsDataplane), &requests))
	t.Cleanup(server.Close)

	client := NewClient(server.URL)
	ctxA := metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "stacks-111"))
	ctxB := metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "stacks-222"))

	client.IsEnabled(ctxA, LogsDataplane)
	client.IsEnabled(ctxB, LogsDataplane)
	client.IsEnabled(ctxA, LogsDataplane)
	require.Equal(t, int64(2), requests.Load())
}

func TestEvaluationContext(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected map[string]any
	}{
		{
			name: "multi-tenant bare stack id normalises to stack namespace",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "123456")),
			expected: map[string]any{
				"targetingKey": "stacks-123456",
				"namespace":    "stacks-123456",
				"stackId":      "123456",
			},
		},
		{
			name: "stacks-prefixed tenant accepted as-is",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "stacks-123456")),
			expected: map[string]any{
				"targetingKey": "stacks-123456",
				"namespace":    "stacks-123456",
				"stackId":      "123456",
			},
		},
		{
			name: "non-stack tenant omits stackId",
			ctx:  metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "some-tenant")),
			expected: map[string]any{
				"targetingKey": "some-tenant",
				"namespace":    "some-tenant",
			},
		},
		{
			name: "single-tenant cloud instance targets by slug from app URL",
			ctx: config.WithGrafanaConfig(context.Background(), config.NewGrafanaCfg(map[string]string{
				config.AppURL: "https://myslug.grafana.net/",
			})),
			expected: map[string]any{
				"targetingKey": "myslug",
				"slug":         "myslug",
			},
		},
		{
			name: "tenant metadata wins over app URL",
			ctx: metadata.NewIncomingContext(
				config.WithGrafanaConfig(context.Background(), config.NewGrafanaCfg(map[string]string{
					config.AppURL: "https://myslug.grafana.net/",
				})),
				metadata.Pairs(tenantIDMetadataKey, "9"),
			),
			expected: map[string]any{
				"targetingKey": "stacks-9",
				"namespace":    "stacks-9",
				"stackId":      "9",
			},
		},
		{
			name: "self-managed app URL yields no targeting attributes",
			ctx: config.WithGrafanaConfig(context.Background(), config.NewGrafanaCfg(map[string]string{
				config.AppURL: "https://grafana.example.com/",
			})),
			expected: map[string]any{},
		},
		{
			name:     "no identifiers yields no targeting attributes",
			ctx:      context.Background(),
			expected: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluationContext(tt.ctx)
			require.Equal(t, len(tt.expected), len(got))
			for k, v := range tt.expected {
				require.Equal(t, v, got[k], "attribute %q", k)
			}
		})
	}
}

func TestEvaluationRequestCarriesContext(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(enabledResponse(LogsDataplane)))
	}))
	t.Cleanup(server.Close)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(tenantIDMetadataKey, "42"))
	require.True(t, NewClient(server.URL).IsEnabled(ctx, LogsDataplane))

	evalCtx, ok := captured["context"].(map[string]any)
	require.True(t, ok, "request body should carry an evaluation context, got %v", captured)
	require.Equal(t, "stacks-42", evalCtx["targetingKey"])
	require.Equal(t, "stacks-42", evalCtx["namespace"])
	require.Equal(t, "42", evalCtx["stackId"])
}

func TestResolveBaseURL(t *testing.T) {
	t.Run("defaults to the in-cluster GOFF service", func(t *testing.T) {
		t.Setenv(ofrepURLEnvVar, "")
		require.Equal(t, defaultOFREPURL, resolveBaseURL())
	})

	t.Run("environment variable overrides the default", func(t *testing.T) {
		t.Setenv(ofrepURLEnvVar, "http://localhost:1031")
		require.Equal(t, "http://localhost:1031", resolveBaseURL())
	})
}

func TestSlugFromAppURL(t *testing.T) {
	tests := []struct {
		appURL   string
		expected string
	}{
		{appURL: "https://myslug.grafana.net/", expected: "myslug"},
		{appURL: "https://myslug.grafana-dev.net", expected: "myslug"},
		{appURL: "https://myslug.grafana-ops.net/", expected: "myslug"},
		{appURL: "https://grafana.example.com/", expected: ""},
		{appURL: "https://grafana.net/", expected: ""},
		{appURL: "://not-a-url", expected: ""},
		{appURL: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.appURL), func(t *testing.T) {
			require.Equal(t, tt.expected, slugFromAppURL(tt.appURL))
		})
	}
}
