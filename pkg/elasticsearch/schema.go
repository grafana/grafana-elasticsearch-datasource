package elasticsearch

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	schemas "github.com/grafana/schemads"
)

var (
	timeRangeOperators = []schemas.Operator{
		schemas.OperatorGreaterThan,
		schemas.OperatorGreaterThanOrEqual,
		schemas.OperatorLessThan,
		schemas.OperatorLessThanOrEqual,
	}
	equalityOperators = []schemas.Operator{
		schemas.OperatorEquals,
		schemas.OperatorNotEquals,
		schemas.OperatorIn,
	}
	searchOperators = []schemas.Operator{
		schemas.OperatorLike,
		schemas.OperatorEquals,
		schemas.OperatorNotEquals,
		schemas.OperatorIn,
	}
)

// SchemaProvider implements schemads handlers: indices as tables plus a fallback table for manual index specification.
type SchemaProvider struct {
	ds *DataSource

	mu              sync.Mutex
	indicesCachedAt time.Time
	indicesCached   []string
	colCache        map[string]columnCacheEntry
}

type columnCacheEntry struct {
	at   time.Time
	cols []schemas.Column
}

// NewSchemaProvider returns a schema provider backed by ds.
func NewSchemaProvider(ds *DataSource) *SchemaProvider {
	return &SchemaProvider{ds: ds, colCache: make(map[string]columnCacheEntry)}
}

func (p *SchemaProvider) Schema(ctx context.Context, _ *schemas.SchemaRequest) (*schemas.SchemaResponse, error) {
	indices, err := p.cachedIndexNames(ctx)

	tables := make([]schemas.Table, 0, len(indices)+1)
	tables = append(tables, fallbackTableSchema())
	for _, idx := range indices {
		tables = append(tables, schemas.Table{Name: idx, Columns: nil})
	}

	var tpv map[string]map[string][]string
	if err != nil {
		tpv = map[string]map[string][]string{
			fallbackTableName: {tableParamIndex: {}},
		}
	} else {
		tpv = map[string]map[string][]string{
			fallbackTableName: {tableParamIndex: indices},
		}
	}

	return &schemas.SchemaResponse{
		FullSchema: &schemas.Schema{
			Tables:               tables,
			TableParameterValues: tpv,
		},
	}, nil
}

func (p *SchemaProvider) Tables(ctx context.Context, _ *schemas.TablesRequest) (*schemas.TablesResponse, error) {
	indices, err := p.cachedIndexNames(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(indices)+1)
	names = append(names, fallbackTableName)
	names = append(names, indices...)
	tp := map[string][]schemas.TableParameter{
		fallbackTableName: fallbackTableParams(),
	}
	return &schemas.TablesResponse{Tables: names, TableParameters: tp}, nil
}

func fallbackTableParams() []schemas.TableParameter {
	return []schemas.TableParameter{
		{Name: tableParamIndex, Root: true, Required: true},
	}
}

func (p *SchemaProvider) Columns(ctx context.Context, req *schemas.ColumnsRequest) (*schemas.ColumnsResponse, error) {
	if req == nil {
		return &schemas.ColumnsResponse{Columns: map[string][]schemas.Column{}}, nil
	}
	out := make(map[string][]schemas.Column, len(req.Tables))
	errs := make(map[string]string)
	tf := p.ds.info.ConfiguredFields.TimeField

	for _, rawName := range req.Tables {
		indexName, err := p.resolveIndexForColumns(rawName, req.TableParameters)
		if err != nil {
			errs[rawName] = err.Error()
			continue
		}
		cols, err := p.cachedFieldCapsColumns(ctx, indexName, tf)
		if err != nil {
			errs[rawName] = err.Error()
			continue
		}
		out[rawName] = cols
	}
	resp := &schemas.ColumnsResponse{Columns: out}
	if len(errs) > 0 {
		resp.Errors = errs
	}
	return resp, nil
}

func (p *SchemaProvider) resolveIndexForColumns(tableName string, tableParams map[string]string) (string, error) {
	if tableName == fallbackTableName {
		if tableParams == nil {
			return "", fmt.Errorf("table %q requires table parameter %q", fallbackTableName, tableParamIndex)
		}
		idx := tableParams[tableParamIndex]
		if idx == "" {
			return "", fmt.Errorf("table %q requires table parameter %q", fallbackTableName, tableParamIndex)
		}
		return idx, nil
	}
	// Allow wildcard patterns (e.g. "logs-*") — field caps supports them natively
	return strings.TrimSpace(tableName), nil
}

func (p *SchemaProvider) TableParameterValues(ctx context.Context, req *schemas.TableParameterValuesRequest) (*schemas.TableParametersValuesResponse, error) {
	if req == nil || req.Table != fallbackTableName || req.TableParameter != tableParamIndex {
		return &schemas.TableParametersValuesResponse{TableParameterValues: map[string][]string{}}, nil
	}
	names, err := p.cachedIndexNames(ctx)
	if err != nil {
		return nil, err
	}
	return &schemas.TableParametersValuesResponse{TableParameterValues: map[string][]string{
		tableParamIndex: names,
	}}, nil
}

func fallbackTableSchema() schemas.Table {
	return schemas.Table{
		Name:            fallbackTableName,
		TableParameters: fallbackTableParams(),
		Columns:         nil,
	}
}

func (p *SchemaProvider) cachedIndexNames(ctx context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.indicesCachedAt) < schemaIndicesCacheTTL && len(p.indicesCached) > 0 {
		return p.indicesCached, nil
	}
	s := p.ds.schemaSettings
	names, err := listAllIndexNames(ctx, p.ds.info, &p.ds.schemaSettings)
	if err != nil {
		return nil, err
	}
	if len(names) > s.MaxIndices {
		names = names[:s.MaxIndices]
	}
	p.indicesCached = names
	p.indicesCachedAt = time.Now()
	return names, nil
}

func (p *SchemaProvider) cachedFieldCapsColumns(ctx context.Context, index string, timeField string) ([]schemas.Column, error) {
	p.mu.Lock()
	entry, ok := p.colCache[index]
	if ok && time.Since(entry.at) < schemaIndicesCacheTTL && len(entry.cols) > 0 {
		p.mu.Unlock()
		return entry.cols, nil
	}
	p.mu.Unlock()

	cols, err := fetchFieldCapsColumns(ctx, p.ds.info, index, timeField, p.ds.schemaSettings.FieldCapsTimeout)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.colCache[index] = columnCacheEntry{at: time.Now(), cols: cols}
	p.mu.Unlock()
	return cols, nil
}
