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

	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Index)
	}
	return filterAndSortIndices(names, s), nil
}

// listIndicesViaResolve uses _resolve/index/* (ES 7.9+) to enumerate concrete
// indices, aliases and data streams in a single lightweight call.
//
// The response contains three top-level arrays that we combine:
//   - "indices": concrete indices. We skip those with a non-empty "data_stream"
//     attribute since they are .ds-* backing indices that the user shouldn't
//     query directly — we surface the data stream name instead.
//   - "aliases": aliases users can query like indices.
//   - "data_streams": user-facing data stream names.
//
// Dedup and hidden-name filtering happen in filterAndSortIndices.
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

	names, err := parseResolveResponse(body)
	if err != nil {
		return nil, err
	}
	return filterAndSortIndices(names, s), nil
}

// parseResolveResponse parses a _resolve/index/* response into a flat list of
// user-facing names: concrete indices (excluding data stream backing indices),
// aliases, and data streams.
func parseResolveResponse(body []byte) ([]string, error) {
	var resp struct {
		Indices []struct {
			Name       string `json:"name"`
			DataStream string `json:"data_stream,omitempty"`
		} `json:"indices"`
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases,omitempty"`
		DataStreams []struct {
			Name string `json:"name"`
		} `json:"data_streams,omitempty"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode resolve indices: %w", err)
	}

	names := make([]string, 0, len(resp.Indices)+len(resp.Aliases)+len(resp.DataStreams))
	for _, idx := range resp.Indices {
		if idx.DataStream != "" {
			continue
		}
		names = append(names, idx.Name)
	}
	for _, a := range resp.Aliases {
		names = append(names, a.Name)
	}
	for _, ds := range resp.DataStreams {
		names = append(names, ds.Name)
	}
	return names, nil
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
