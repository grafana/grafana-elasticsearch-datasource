package elasticsearch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/experimental/featuretoggles"
	schemas "github.com/grafana/schemads"
	"github.com/stretchr/testify/require"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
)

func TestNormalizeGrafanaSQLRequest_PassthroughWithoutToggle(t *testing.T) {
	ds := &DataSource{
		info: &es.DatasourceInfo{
			ConfiguredFields: es.ConfiguredFields{TimeField: "@timestamp"},
		},
	}
	raw := []byte(`{"refId":"A","query":"*","grafanaSql":true,"table":"my-index"}`)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: backend.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: "",
			}),
		},
		Queries: []backend.DataQuery{{RefID: "A", JSON: raw}},
	}
	out := normalizeGrafanaSQLRequest(backend.Logger, ds, req)
	require.JSONEq(t, string(raw), string(out.Queries[0].JSON))
}

func TestNormalizeGrafanaSQLRequest_IndexTable(t *testing.T) {
	ds := &DataSource{
		info: &es.DatasourceInfo{
			ConfiguredFields: es.ConfiguredFields{TimeField: "@timestamp"},
		},
	}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(map[string]any{
		"refId":      "A",
		"grafanaSql": true,
		"table":      "logs-0001",
		"filters":    []any{},
	})
	require.NoError(t, err)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: backend.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: dsAbstractionAppFeature,
			}),
		},
		Queries: []backend.DataQuery{{
			RefID:     "A",
			TimeRange: backend.TimeRange{From: from, To: to},
			JSON:      raw,
		}},
	}
	out := normalizeGrafanaSQLRequest(backend.Logger, ds, req)
	require.Len(t, out.Queries, 1)
	require.Equal(t, "lucene", out.Queries[0].QueryType)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &m))
	require.Equal(t, "logs-0001", m["index"])
}

func TestNormalizeGrafanaSQLRequest_FallbackTable(t *testing.T) {
	ds := &DataSource{
		info: &es.DatasourceInfo{
			ConfiguredFields: es.ConfiguredFields{TimeField: "@timestamp"},
		},
	}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(map[string]any{
		"refId":                "A",
		"grafanaSql":           true,
		"table":                fallbackTableName,
		"tableParameterValues": map[string]any{"index": "my-idx"},
		"filters":              []any{},
	})
	require.NoError(t, err)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: backend.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: dsAbstractionAppFeature,
			}),
		},
		Queries: []backend.DataQuery{{
			RefID:     "A",
			TimeRange: backend.TimeRange{From: from, To: to},
			JSON:      raw,
		}},
	}
	out := normalizeGrafanaSQLRequest(backend.Logger, ds, req)
	require.Len(t, out.Queries, 1)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &m))
	require.Equal(t, "my-idx", m["index"])
}

func TestMergeTimeRangeFromFilters(t *testing.T) {
	from := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)
	cf := es.ConfiguredFields{TimeField: "@ts"}
	tr := mergeTimeRangeFromFilters(backend.TimeRange{From: from, To: to}, nil, cf)
	require.Equal(t, from, tr.From)
	require.Equal(t, to, tr.To)
}

func TestBuildBoolFilters_RangeFilter(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "status_code",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorGreaterThanOrEqual, Value: "200"},
				{Operator: schemas.OperatorLessThan, Value: "300"},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Len(t, bf.Filter, 2)
	require.Empty(t, bf.MustNot)
	require.Equal(t, "range", bf.Filter[0].Type)
	require.Equal(t, "status_code", bf.Filter[0].Field)
	require.Equal(t, "200", bf.Filter[0].Bounds["gte"])
	require.Equal(t, "range", bf.Filter[1].Type)
	require.Equal(t, "300", bf.Filter[1].Bounds["lt"])
}

func TestBuildBoolFilters_NotEquals(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "level",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorNotEquals, Value: "debug"},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Empty(t, bf.Filter)
	require.Len(t, bf.MustNot, 1)
	require.Equal(t, "match_phrase", bf.MustNot[0].Type)
	require.Equal(t, "level", bf.MustNot[0].Field)
	require.Equal(t, "debug", bf.MustNot[0].Value)
}

func TestBuildBoolFilters_InFilter(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "host",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorIn, Values: []any{"web-1", "web-2", "web-3"}},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Len(t, bf.Filter, 1)
	require.Equal(t, "terms", bf.Filter[0].Type)
	require.Equal(t, "host", bf.Filter[0].Field)
	require.Equal(t, []any{"web-1", "web-2", "web-3"}, bf.Filter[0].Values)
}

func TestBuildBoolFilters_LikeFilter(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "message",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorLike, Value: "%error%"},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Len(t, bf.Filter, 1)
	require.Equal(t, "wildcard", bf.Filter[0].Type)
	require.Equal(t, "message", bf.Filter[0].Field)
	require.Equal(t, "*error*", bf.Filter[0].Value)
}

func TestBuildBoolFilters_Combined(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "level",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorEquals, Value: "error"},
			},
		},
		{
			Name: "env",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorNotEquals, Value: "test"},
			},
		},
		{
			Name: "code",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorGreaterThanOrEqual, Value: "500"},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Len(t, bf.Filter, 2)
	require.Len(t, bf.MustNot, 1)
	require.Equal(t, "match_phrase", bf.Filter[0].Type)
	require.Equal(t, "level", bf.Filter[0].Field)
	require.Equal(t, "range", bf.Filter[1].Type)
	require.Equal(t, "code", bf.Filter[1].Field)
	require.Equal(t, "match_phrase", bf.MustNot[0].Type)
	require.Equal(t, "env", bf.MustNot[0].Field)
}

func TestBuildBoolFilters_SkipsTimeColumn(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "@timestamp",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorGreaterThan, Value: "2024-01-01"},
			},
		},
		{
			Name: "level",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorEquals, Value: "warn"},
			},
		},
	}
	bf := buildBoolFilters(cf, filters)
	require.NotNil(t, bf)
	require.Len(t, bf.Filter, 1)
	require.Equal(t, "level", bf.Filter[0].Field)
}

func TestBuildLuceneGrafanaSQL_HasBoolFilters(t *testing.T) {
	cf := es.ConfiguredFields{TimeField: "@timestamp"}
	filters := []schemas.ColumnFilter{
		{
			Name: "status",
			Conditions: []schemas.FilterCondition{
				{Operator: schemas.OperatorEquals, Value: "error"},
			},
		},
	}
	model, err := buildLuceneGrafanaSQL("A", "my-index", cf, filters)
	require.NoError(t, err)
	require.Equal(t, "*", model["query"])
	require.Equal(t, "my-index", model["index"])
	bf, ok := model["boolFilters"].(*boolFiltersJSON)
	require.True(t, ok)
	require.Len(t, bf.Filter, 1)
	require.Equal(t, "match_phrase", bf.Filter[0].Type)
}

func TestNormalizeGrafanaSQLRequest_LuceneBoolFilters(t *testing.T) {
	ds := &DataSource{
		info: &es.DatasourceInfo{
			ConfiguredFields: es.ConfiguredFields{TimeField: "@timestamp"},
		},
	}
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(map[string]any{
		"refId":      "A",
		"grafanaSql": true,
		"table":      "logs-0001",
		"filters": []map[string]any{
			{
				"name": "status",
				"conditions": []map[string]any{
					{"operator": ">=", "value": "500"},
				},
			},
		},
	})
	require.NoError(t, err)
	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			GrafanaConfig: backend.NewGrafanaCfg(map[string]string{
				featuretoggles.EnabledFeatures: dsAbstractionAppFeature,
			}),
		},
		Queries: []backend.DataQuery{{
			RefID:     "A",
			TimeRange: backend.TimeRange{From: from, To: to},
			JSON:      raw,
		}},
	}
	out := normalizeGrafanaSQLRequest(backend.Logger, ds, req)
	require.Len(t, out.Queries, 1)
	require.Equal(t, "lucene", out.Queries[0].QueryType)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out.Queries[0].JSON, &m))
	require.Equal(t, "logs-0001", m["index"])
	bf, ok := m["boolFilters"].(map[string]any)
	require.True(t, ok)
	filterArr, ok := bf["filter"].([]any)
	require.True(t, ok)
	require.Len(t, filterArr, 1)
}
