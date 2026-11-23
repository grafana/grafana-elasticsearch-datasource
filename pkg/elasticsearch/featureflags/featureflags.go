// Package featureflags evaluates feature flags over the OpenFeature Remote
// Evaluation Protocol (OFREP).
//
// Grafana instance feature toggles (GF_INSTANCE_FEATURE_TOGGLES_ENABLE) are a
// separate system: a flag defined only in Grafana Cloud's feature-flag
// configuration never appears there, so plugin backends evaluate those flags
// over OFREP directly. A flag must exist (disabled) in every wave before code
// evaluating it ships: an undefined flag turns every evaluation into an error
// result, cached and logged once per flag, tenant, and TTL window.
//
// The OFREP endpoint, its cache TTL and the targeting attributes come from the
// host's OpenFeature advertisement in each request's Grafana configuration.
// Where the host advertises no endpoint the plugin can use (Grafana before
// 13.3, or a provider type other than ofrep), the plugin falls back to the
// GOFF relay proxy or GF_PLUGIN_ELASTICSEARCH_OFREP_URL and derives the tenant
// itself.
//
// Evaluation fails closed: with no previous answer, any transport or
// evaluation error yields false. A failed refresh keeps the previous answer
// only for a GENERAL error (transport errors and HTTP 401, 403, 429 and 5xx),
// so a flag removed from the endpoint turns off at the next refresh.
// An error result expires after the TTL or one minute, whichever is shorter.
// Results are cached per flag, endpoint and tenant, so a query pays at most one
// OFREP round trip per flag, endpoint, tenant, and TTL window. That round trip
// runs on the query path, bounded by dialTimeout for name resolution and
// connect and by requestTimeout overall.
package featureflags

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
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

// MetricsDataplane gates emission of Grafana dataplane-compliant metrics
// frames. The key must match the GOFF flag definition byte-for-byte.
const MetricsDataplane = "elasticsearch.metrics-dataplane"

const (
	// defaultOFREPURL is the unauthenticated GOFF relay proxy. Anywhere else the
	// host does not resolve and evaluations fail closed.
	defaultOFREPURL = "http://go-feature-flag.hosted-grafana.svc.cluster.local"

	// ofrepURLEnvVar overrides the fallback OFREP base URL for local
	// development and end-to-end tests on hosts that advertise no endpoint.
	ofrepURLEnvVar = "GF_PLUGIN_ELASTICSEARCH_OFREP_URL"

	// requestTimeout matches the timeout other in-cluster services use against
	// the same relay proxy.
	requestTimeout = 5 * time.Second

	// dialTimeout bounds name resolution and connect separately. Where the
	// default in-cluster host is unreachable by design (OSS, self-managed),
	// some resolvers take the whole requestTimeout to fail it (for example
	// ones routing .local to mDNS), and that wait would land on the first
	// query of every TTL window.
	dialTimeout = time.Second

	// defaultCacheTTL matches the interval at which the relay proxy reloads
	// flag definitions. It applies when the host gives no caching advice.
	defaultCacheTTL = time.Minute

	// tenantIDMetadataKey is the gRPC metadata key carrying the tenant
	// identifier on multi-tenant requests (the bare numeric stack id), the
	// same key the plugin SDK's tenant middleware reads. It identifies the
	// tenant only on requests whose host advertises no usable endpoint.
	tenantIDMetadataKey = "tenantID"
)

// Client evaluates boolean feature flags over OFREP, caching results per flag,
// endpoint and targeting key. The zero value is not usable; construct with NewClient.
type Client struct {
	logger log.Logger
	// fallbackURL serves requests whose host advertises no usable endpoint.
	fallbackURL string
	// explicitURL records that fallbackURL was named by a caller or an
	// operator rather than defaulted, so a failure to reach it is noteworthy.
	explicitURL bool
	group       singleflight.Group

	providersMu sync.Mutex
	providers   map[string]openfeature.FeatureProvider

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

type cacheEntry struct {
	value   bool
	expires time.Time
}

type target struct {
	url     string
	ttl     time.Duration
	evalCtx openfeature.FlattenedContext
	// routine marks failures that are expected, so they log at debug level.
	routine bool
}

// NewClient returns a Client that evaluates flags against the endpoint the
// host advertises, or against fallbackURL when the host advertises none. A
// caller that names a URL expects it to answer, so failures against it are
// logged as warnings.
func NewClient(fallbackURL string) *Client {
	return &Client{
		logger:      log.DefaultLogger,
		fallbackURL: fallbackURL,
		explicitURL: true,
		providers:   map[string]openfeature.FeatureProvider{},
		cache:       map[string]cacheEntry{},
	}
}

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: dialTimeout}).DialContext
	return &http.Client{Timeout: requestTimeout, Transport: transport}
}

var defaultClient = sync.OnceValue(newDefaultClient)

func newDefaultClient() *Client {
	baseURL, explicit := resolveBaseURL()
	c := NewClient(baseURL)
	c.explicitURL = explicit
	return c
}

// IsEnabled evaluates flag against the process-wide default client. It returns false on any error.
func IsEnabled(ctx context.Context, flag string) bool {
	return defaultClient().IsEnabled(ctx, flag)
}

// IsEnabled evaluates flag for the tenant of ctx, returning false on any error
// when no previous answer exists. Concurrent lookups for the same flag,
// endpoint and tenant are collapsed into one request, which does not inherit
// ctx's cancellation because its result is shared with every caller in the window.
func (c *Client) IsEnabled(ctx context.Context, flag string) bool {
	t := c.target(ctx)
	key := cacheKey(flag, t)
	if entry, fresh := c.cached(key); fresh {
		evaluationsTotal.WithLabelValues(flag, outcomeHit).Inc()
		return entry.value
	}
	return c.evaluate(ctx, key, flag, t).value
}

// target uses the host's advertised endpoint when the plugin can use it, else the fallback URL.
func (c *Client) target(ctx context.Context) target {
	if of, ok := advertisedEndpoint(ctx, c.logger); ok {
		return target{url: of.URL, ttl: cacheTTL(of), evalCtx: advertisedEvaluationContext(ctx, of.ContextAttrs)}
	}
	evalCtx := evaluationContext(ctx)
	return target{
		url:     c.fallbackURL,
		ttl:     defaultCacheTTL,
		evalCtx: evalCtx,
		routine: routineFailure(c.explicitURL, evalCtx),
	}
}

func advertisedEndpoint(ctx context.Context, logger log.Logger) (config.OpenFeatureConfig, bool) {
	of, err := config.GrafanaConfigFromContext(ctx).OpenFeature()
	if err != nil {
		if !errors.Is(err, config.ErrOpenFeatureNotConfigured) {
			logger.FromContext(ctx).Debug("Invalid OpenFeature configuration from the host", "error", err)
		}
		return of, false
	}
	// Plugins have no credentials for features-service, Grafana logs two errors for each evaluation of a flag
	// that its static provider does not define, and a type newer than the plugin can need credentials too.
	return of, of.ProviderType == "ofrep"
}

func (c *Client) cached(key string) (cacheEntry, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	entry, ok := c.cache[key]
	return entry, ok && time.Now().Before(entry.expires)
}

// evaluate runs one OFREP request per key at a time and caches its answer for the target's TTL.
// A failed request caches false, or the previous answer after a GENERAL error, for at most defaultCacheTTL.
func (c *Client) evaluate(ctx context.Context, key, flag string, t target) cacheEntry {
	v, _, _ := c.group.Do(key, func() (any, error) {
		previous, fresh := c.cached(key)
		if fresh {
			evaluationsTotal.WithLabelValues(flag, outcomeHit).Inc()
			return previous, nil
		}

		// A query canceled mid-evaluation must not cache a spurious failure
		// for the tenant's whole TTL window, so the request runs detached from
		// the caller's cancellation. The request timeout still bounds it.
		detached := context.WithoutCancel(ctx)
		start := time.Now()
		detail := c.provider(t.url).BooleanEvaluation(detached, flag, false, t.evalCtx)
		requestDurationSeconds.WithLabelValues(flag).Observe(time.Since(start).Seconds())

		entry := cacheEntry{value: detail.Value}
		ttl := t.ttl
		outcome := outcomeMiss
		if detail.Reason == openfeature.ErrorReason {
			outcome = outcomeError
			logFailure := c.logger.FromContext(ctx).Warn
			if t.routine {
				logFailure = c.logger.FromContext(ctx).Debug
			}
			logFailure("Feature flag evaluation failed", "flag", flag, "error", detail.ResolutionError.Error())
			if detail.ResolutionDetail().ErrorCode == openfeature.GeneralCode {
				entry.value = previous.value
			}
			ttl = min(ttl, defaultCacheTTL)
		}
		entry.expires = time.Now().Add(ttl)
		evaluationsTotal.WithLabelValues(flag, outcome).Inc()

		c.cacheMu.Lock()
		c.cache[key] = entry
		c.cacheMu.Unlock()
		return entry, nil
	})
	return v.(cacheEntry)
}

func (c *Client) provider(endpoint string) openfeature.FeatureProvider {
	c.providersMu.Lock()
	defer c.providersMu.Unlock()
	p, ok := c.providers[endpoint]
	if !ok {
		// The provider ignores WithTimeout once a client is supplied, so the client carries the bound.
		p = ofrep.NewProvider(endpoint, ofrep.WithClient(newHTTPClient()))
		c.providers[endpoint] = p
	}
	return p
}

// advertisedEvaluationContext targets the request's namespace and adds the host's attributes, which win on a name clash.
func advertisedEvaluationContext(ctx context.Context, hostAttrs map[string]string) openfeature.FlattenedContext {
	namespace := backend.PluginConfigFromContext(ctx).Namespace
	if namespace == "" {
		namespace = hostAttrs["namespace"]
	}

	evalCtx := make(openfeature.FlattenedContext, len(hostAttrs)+2)
	if namespace != "" {
		evalCtx[openfeature.TargetingKey] = namespace
		evalCtx["namespace"] = namespace
	}
	for k, v := range hostAttrs {
		evalCtx[k] = v
	}
	return evalCtx
}

func cacheTTL(of config.OpenFeatureConfig) time.Duration {
	if of.CacheTTL > 0 {
		return of.CacheTTL
	}
	return defaultCacheTTL
}

// evaluationContext builds the OFREP targeting attributes for a request whose
// host advertises no usable endpoint. Multi-tenant requests carry the tenant
// identifier in gRPC metadata; single-tenant cloud instances are identified by
// the stack slug in the Grafana app URL. Elsewhere (OSS, self-managed) no
// attributes exist and only a flag's default rule can match.
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

// routineFailure reports whether an evaluation failure against the fallback
// URL is expected rather than noteworthy: nobody configured the OFREP URL and
// the request carries no Grafana Cloud identity, so no OFREP service is
// expected to exist (OSS, self-managed, local development). Warning there
// would repeat once per TTL window for the life of the process.
func routineFailure(explicitURL bool, evalCtx openfeature.FlattenedContext) bool {
	_, cloud := evalCtx[openfeature.TargetingKey]
	return !explicitURL && !cloud
}

func cacheKey(flag string, t target) string {
	targetingKey, _ := t.evalCtx[openfeature.TargetingKey].(string)
	return flag + "|" + t.url + "|" + targetingKey
}

// resolveBaseURL returns the fallback OFREP base URL and whether an operator set it.
func resolveBaseURL() (baseURL string, explicit bool) {
	if fromEnv := os.Getenv(ofrepURLEnvVar); fromEnv != "" {
		return fromEnv, true
	}
	return defaultOFREPURL, false
}
