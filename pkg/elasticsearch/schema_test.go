package elasticsearch

import (
	"context"
	"fmt"
	"testing"

	schemas "github.com/grafana/schemads"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func TestNormalizeTableNameForLookup(t *testing.T) {
	require.Equal(t, "my_index", normalizeTableNameForLookup("my_index"))
	require.Equal(t, "logs-2024", normalizeTableNameForLookup("logs-2024"))
}

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
	idx, err := p.resolveIndexForColumns("my-index", nil)
	require.NoError(t, err)
	require.Equal(t, "my-index", idx)

	_, err = p.resolveIndexForColumns(fallbackTableName, nil)
	require.Error(t, err)

	idx, err = p.resolveIndexForColumns(fallbackTableName, map[string]string{tableParamIndex: "real-index"})
	require.NoError(t, err)
	require.Equal(t, "real-index", idx)
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
	rows := []catIndexRow{
		{Index: ".kibana"},
		{Index: "logs-prod"},
		{Index: "logs-staging"},
		{Index: "metrics"},
		{Index: ""},
	}
	s := defaultSchemaSettings()
	names := filterAndSortIndices(rows, &s)
	require.Equal(t, []string{"logs-prod", "logs-staging", "metrics"}, names)
}

func TestFilterAndSortIndices_IncludeHidden(t *testing.T) {
	rows := []catIndexRow{
		{Index: ".kibana"},
		{Index: "logs"},
	}
	s := defaultSchemaSettings()
	s.IncludeHidden = true
	names := filterAndSortIndices(rows, &s)
	require.Equal(t, []string{".kibana", "logs"}, names)
}

func TestIsForbiddenOrUnauthorized(t *testing.T) {
	require.True(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 403: forbidden")))
	require.True(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 401: unauthorized")))
	require.False(t, isForbiddenOrUnauthorized(fmt.Errorf("list indices: HTTP 500: internal")))
}
