package client

import (
	"net/http"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/stretchr/testify/require"
)

// productHeaderInjector wraps an http.RoundTripper and ensures every response
// carries the X-Elastic-Product header the go-elasticsearch client uses to
// verify it is talking to a real Elasticsearch server. Test servers don't
// emit this header, so we add it transparently in tests.
type productHeaderInjector struct {
	inner http.RoundTripper
}

func (p *productHeaderInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := p.inner.RoundTrip(req)
	if err != nil || res == nil {
		return res, err
	}
	if res.Header == nil {
		res.Header = http.Header{}
	}
	if res.Header.Get("X-Elastic-Product") == "" {
		res.Header.Set("X-Elastic-Product", "Elasticsearch")
	}
	return res, nil
}

// newTestESClient builds an elasticsearch.Client whose transport is wired to
// the provided http.Client (typically an httptest.Server client). Tests use
// this so they can assert against the raw HTTP request that the client
// eventually issues against the fake server. The transport injects the
// X-Elastic-Product header the client library requires.
func newTestESClient(t *testing.T, httpCli *http.Client, url string) *elasticsearch.Client {
	t.Helper()
	inner := httpCli.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	wrapped := &http.Client{
		Transport:     &productHeaderInjector{inner: inner},
		CheckRedirect: httpCli.CheckRedirect,
		Jar:           httpCli.Jar,
		Timeout:       httpCli.Timeout,
	}
	cli, err := NewESClient(wrapped, url)
	require.NoError(t, err)
	return cli
}
