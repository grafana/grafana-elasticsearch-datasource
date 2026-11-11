package client

import (
	"fmt"
	"net/http"

	"github.com/elastic/go-elasticsearch/v8"
)

// NewESClient builds an official Elasticsearch client that routes every request
// through the Grafana plugin-SDK http.Client's RoundTripper. That keeps all the
// middleware the SDK gives us (TLS, proxy, Basic/API-Key/SigV4 auth, custom
// headers, tracing) in place while letting the official client own URL
// construction and typed API surface.
//
// Retries are disabled so behaviour matches the previous bespoke transport,
// which issued each request exactly once.
func NewESClient(httpCli *http.Client, url string) (*elasticsearch.Client, error) {
	if httpCli == nil {
		return nil, fmt.Errorf("http client is required to build elasticsearch client")
	}
	cfg := elasticsearch.Config{
		Addresses:         []string{url},
		Transport:         httpCli.Transport,
		DisableRetry:      true,
		DisableMetaHeader: true,
	}
	return elasticsearch.NewClient(cfg)
}
