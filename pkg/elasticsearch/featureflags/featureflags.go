// Package featureflags evaluates Grafana Cloud feature flags over the
// OpenFeature Remote Evaluation Protocol (OFREP) against the GOFF
// (go-feature-flag) relay proxy operated alongside hosted Grafana.
//
// Grafana instance feature toggles (GF_INSTANCE_FEATURE_TOGGLES_ENABLE) are a
// separate system: a flag defined only in GOFF never appears there, so plugin
// backends evaluate GOFF flags over OFREP directly. Flag definitions are
// managed centrally in Grafana Cloud's feature-flag configuration and roll
// out wave by wave. A flag must exist (disabled) in every wave before code
// evaluating it ships, because undefined flags cause an uncached error on
// every evaluation.
//
// Evaluation fails closed: any transport or evaluation error yields false, so
// environments with no reachable GOFF service (OSS, self-managed, local
// development) keep flagged behaviour off until the flag's in-code default
// flips. Results are cached per flag and tenant for cacheTTL, keeping
// evaluation off the query hot path.
package featureflags

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/config"
	"github.com/open-feature/go-sdk-contrib/providers/ofrep"
	"github.com/open-feature/go-sdk/openfeature"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/metadata"
)

// LogsDataplane gates emission of Grafana dataplane-compliant logs frames.
// The key must match the GOFF flag definition byte-for-byte.
const LogsDataplane = "elasticsearch.logs-dataplane"

const (
	// defaultOFREPURL is the unauthenticated GOFF relay proxy reachable from
	// hosted-grafana and grafana-datasources namespace pods. Anywhere else the
	// host does not resolve and evaluations fail closed.
	defaultOFREPURL = "http://go-feature-flag.hosted-grafana.svc.cluster.local"

	// ofrepURLEnvVar overrides the OFREP base URL for local development and
	// end-to-end tests.
	ofrepURLEnvVar = "GF_PLUGIN_ELASTICSEARCH_OFREP_URL"

	// requestTimeout matches the goff_request_timeout used by hosted-grafana
	// services talking to the same relay proxy.
	requestTimeout = 5 * time.Second

	// cacheTTL matches the interval at which the relay proxy reloads flag
	// definitions, so a flag flip is picked up within about a minute. Errors
	// are cached for the same period so an unreachable service costs one
	// failed request per flag, tenant, and TTL window rather than one per query.
	cacheTTL = time.Minute

	// tenantIDMetadataKey is the gRPC metadata key carrying the tenant
	// identifier on multi-tenant requests (the bare numeric stack id), the
	// same key the plugin SDK's tenant middleware reads.
	tenantIDMetadataKey = "tenantID"
)

// Client evaluates boolean feature flags over OFREP, caching results per flag
// and targeting key. The zero value is not usable; construct with NewClient.
type Client struct {
	provider openfeature.FeatureProvider
	logger   log.Logger
	group    singleflight.Group
	mu       sync.RWMutex
	cache    map[string]cacheEntry
}

type cacheEntry struct {
	value   bool
	expires time.Time
}

// NewClient returns a Client evaluating flags against the OFREP service at
// baseURL.
func NewClient(baseURL string) *Client {
	return &Client{
		provider: ofrep.NewProvider(baseURL, ofrep.WithTimeout(requestTimeout)),
		logger:   log.DefaultLogger,
		cache:    map[string]cacheEntry{},
	}
}

var defaultClient = sync.OnceValue(func() *Client {
	return NewClient(resolveBaseURL())
})

// IsEnabled evaluates flag against the process-wide default client, resolving
// the OFREP base URL from GF_PLUGIN_ELASTICSEARCH_OFREP_URL or falling back
// to the in-cluster GOFF service. It returns false on any error.
func IsEnabled(ctx context.Context, flag string) bool {
	return defaultClient().IsEnabled(ctx, flag)
}

// IsEnabled evaluates flag with targeting attributes derived from ctx,
// returning false on any error. Results, including failures, are cached for
// cacheTTL per flag and targeting key, with concurrent lookups for the same
// key collapsed into one request.
func (c *Client) IsEnabled(ctx context.Context, flag string) bool {
	evalCtx := evaluationContext(ctx)
	key := cacheKey(flag, evalCtx)

	c.mu.RLock()
	entry, ok := c.cache[key]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.value
	}

	value, _, _ := c.group.Do(key, func() (any, error) {
		detail := c.provider.BooleanEvaluation(ctx, flag, false, evalCtx)
		if detail.Reason == openfeature.ErrorReason {
			c.logger.FromContext(ctx).Warn("Feature flag evaluation failed, defaulting to off",
				"flag", flag, "error", detail.ResolutionError.Error())
		}
		c.mu.Lock()
		c.cache[key] = cacheEntry{value: detail.Value, expires: time.Now().Add(cacheTTL)}
		c.mu.Unlock()
		return detail.Value, nil
	})
	return value.(bool)
}

// evaluationContext builds the OFREP targeting attributes available to the
// plugin backend. Multi-tenant requests carry the tenant identifier in gRPC
// metadata; single-tenant cloud instances are identified by the stack slug in
// the Grafana app URL. Elsewhere (OSS, self-managed) no attributes exist and
// only a flag's default rule can match.
func evaluationContext(ctx context.Context) openfeature.FlattenedContext {
	evalCtx := openfeature.FlattenedContext{}

	if tenant := tenantFromMetadata(ctx); tenant != "" {
		namespace, stackID := tenantAttributes(tenant)
		evalCtx[openfeature.TargetingKey] = namespace
		evalCtx["namespace"] = namespace
		if stackID != "" {
			evalCtx["stackId"] = stackID
		}
		return evalCtx
	}

	appURL, err := config.GrafanaConfigFromContext(ctx).AppURL()
	if err != nil {
		return evalCtx
	}
	if slug := slugFromAppURL(appURL); slug != "" {
		evalCtx[openfeature.TargetingKey] = slug
		evalCtx["slug"] = slug
	}
	return evalCtx
}

// tenantFromMetadata returns the raw tenant identifier from incoming gRPC
// metadata, or "" outside multi-tenant deployments.
func tenantFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if values := md.Get(tenantIDMetadataKey); len(values) > 0 {
		return values[0]
	}
	return ""
}

// tenantAttributes normalises a tenant identifier into the stacks-<id>
// namespace form (the host's OpenFeature targeting key) and the bare stack
// id. The multi-tenant runner puts the bare numeric stack id on the wire;
// the stacks-<id> namespace form is accepted defensively. Anything else is
// used as the namespace verbatim with no stack id.
func tenantAttributes(tenant string) (namespace, stackID string) {
	if id, ok := strings.CutPrefix(tenant, "stacks-"); ok {
		return tenant, id
	}
	if isAllDigits(tenant) {
		return "stacks-" + tenant, tenant
	}
	return tenant, ""
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// slugFromAppURL extracts the stack slug from a Grafana Cloud app URL
// (<slug>.grafana.net and the dev/ops equivalents). Any other host returns ""
// so self-managed hostnames never masquerade as cloud slugs.
func slugFromAppURL(appURL string) string {
	parsed, err := url.Parse(appURL)
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	for _, suffix := range []string{".grafana.net", ".grafana-dev.net", ".grafana-ops.net"} {
		if slug, ok := strings.CutSuffix(host, suffix); ok && slug != "" && !strings.Contains(slug, ".") {
			return slug
		}
	}
	return ""
}

func cacheKey(flag string, evalCtx openfeature.FlattenedContext) string {
	targetingKey, _ := evalCtx[openfeature.TargetingKey].(string)
	return flag + "|" + targetingKey
}

func resolveBaseURL() string {
	if fromEnv := os.Getenv(ofrepURLEnvVar); fromEnv != "" {
		return fromEnv
	}
	return defaultOFREPURL
}
