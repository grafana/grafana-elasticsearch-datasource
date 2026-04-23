package elasticsearch

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	schemas "github.com/grafana/schemads"
)

// readResponseBody reads an HTTP response body, transparently decompressing
// gzip-encoded payloads. Go's net/http only auto-decompresses when the stdlib
// itself added Accept-Encoding: gzip; Grafana SDK HTTP middlewares (e.g. SigV4)
// can pre-set headers or wrap the transport in ways that disable that, so we
// have to honor Content-Encoding explicitly.
func readResponseBody(resp *http.Response) ([]byte, error) {
	reader := resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		defer func() { _ = gr.Close() }()
		reader = gr
	}
	return io.ReadAll(reader)
}

func listIndicesViaCat(ctx context.Context, info *es.DatasourceInfo, s *schemaSettings) ([]string, error) {
	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, fmt.Errorf("parse ES URL: %w", err)
	}
	u.Path = path.Join(u.Path, "_cat/indices")
	q := u.Query()
	q.Set("format", "json")
	q.Set("expand_wildcards", "open")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := info.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, fmt.Errorf("list indices: HTTP %d: %s", resp.StatusCode, truncateForErr(body))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list indices: HTTP %d: %s", resp.StatusCode, truncateForErr(body))
	}

	var rows []catIndexRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode cat indices: %w", err)
	}

	return filterAndSortIndices(rows, s), nil
}

// listIndicesViaResolve uses _resolve/index/* as a fallback when _cat/indices is forbidden (ES 7.9+).
func listIndicesViaResolve(ctx context.Context, info *es.DatasourceInfo, s *schemaSettings) ([]string, error) {
	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, fmt.Errorf("parse ES URL: %w", err)
	}
	u.Path = path.Join(u.Path, "_resolve/index/*")
	q := u.Query()
	q.Set("expand_wildcards", "open")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := info.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("resolve indices: HTTP %d: %s", resp.StatusCode, truncateForErr(body))
	}

	var resolveResp struct {
		Indices []struct {
			Name string `json:"name"`
		} `json:"indices"`
	}
	if err := json.Unmarshal(body, &resolveResp); err != nil {
		return nil, fmt.Errorf("decode resolve indices: %w", err)
	}

	rows := make([]catIndexRow, 0, len(resolveResp.Indices))
	for _, idx := range resolveResp.Indices {
		rows = append(rows, catIndexRow{Index: idx.Name})
	}
	return filterAndSortIndices(rows, s), nil
}

// fetchFieldCapsColumns loads field caps for a single index (or pattern) and returns schemads columns.
func fetchFieldCapsColumns(ctx context.Context, info *es.DatasourceInfo, index string, timeField string, timeout time.Duration) ([]schemas.Column, error) {
	if strings.Contains(index, ",") {
		return nil, fmt.Errorf("multi-index not supported in schema column fetch")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(u.Path, index, "_field_caps")
	q := u.Query()
	q.Set("fields", "*")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := info.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("field_caps: HTTP %d: %s", resp.StatusCode, truncateForErr(body))
	}
	return fieldCapsToColumns(body, timeField)
}
