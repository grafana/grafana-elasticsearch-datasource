package elasticsearch

import (
	"encoding/json"
	"fmt"
	"strconv"

	es "github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/client"
	"github.com/grafana/grafana-elasticsearch-datasource/pkg/elasticsearch/simplejson"
	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// processQuery processes a single query and adds it to the multi-search request builder
func (e *elasticsearchDataQuery) processQuery(q *Query, ms *es.MultiSearchRequestBuilder, from, to int64) error {
	err := isQueryWithError(q)
	if err != nil {
		return backend.DownstreamError(fmt.Errorf("received invalid query. %w", err))
	}

	defaultTimeField := e.client.GetConfiguredFields().TimeField
	b := ms.Search(clampAutoInterval(q, from, to), q.TimeRange)
	if q.Index != "" {
		b.SetIndex(q.Index)
	}
	b.Size(0)
	filters := b.Query().Bool().Filter()
	filters.AddDateRangeFilter(defaultTimeField, to, from, es.DateFormatEpochMS)

	if q.IsDSLQuery() {
		if err := e.processRawDSLQuery(q, b); err != nil {
			return err
		}
	} else {
		// For non-DSL queries (Lucene), add the query string filter
		filters.AddQueryStringFilter(q.RawQuery, true)
	}

	if q.BoolFilters != nil {
		applyBoolFilters(q.BoolFilters, b)
	}

	if len(q.SourceIncludes) > 0 {
		b.SetSourceIncludes(q.SourceIncludes)
	}

	if isLogsQuery(q) {
		processLogsQuery(q, b, from, to, defaultTimeField)
	} else if isDocumentQuery(q) {
		processDocumentQuery(q, b, from, to, defaultTimeField)
	} else {
		// Otherwise, it is a time series query and we process it
		processTimeSeriesQuery(q, b, from, to, defaultTimeField)
	}

	return nil
}

// processLogsQuery processes a logs query and configures the search request accordingly
func processLogsQuery(q *Query, b *es.SearchRequestBuilder, from, to int64, defaultTimeField string) {
	metric := q.Metrics[0]
	sort := es.SortOrderDesc
	if metric.Settings.Get("sortDirection").MustString() == "asc" {
		// This is currently used only for log context query
		sort = es.SortOrderAsc
	}
	b.Sort(sort, defaultTimeField, "boolean")
	b.Sort(sort, "_doc", "")
	b.AddDocValueField(defaultTimeField)
	// We need to add timeField as field with standardized time format to not receive
	// invalid formats that elasticsearch can parse, but our frontend can't (e.g. yyyy_MM_dd_HH_mm_ss)
	b.AddTimeFieldWithStandardizedFormat(defaultTimeField)
	b.Size(stringToIntWithDefaultValue(metric.Settings.Get("limit").MustString(), defaultSize))
	b.AddHighlight()

	if q.IncludeRuntimeFields {
		b.EnableRuntimeFields()
	}

	// This is currently used only for log context query to get
	// log lines before and after the selected log line
	searchAfter := metric.Settings.Get("searchAfter").MustArray()
	for _, value := range searchAfter {
		b.AddSearchAfter(value)
	}

	// For log query, we add a date histogram aggregation
	aggBuilder := b.Agg()
	bucketAgg := &BucketAgg{
		Type:  dateHistType,
		Field: defaultTimeField,
		ID:    "1",
		Settings: simplejson.NewFromAny(map[string]any{
			"interval": "auto",
		}),
	}
	q.BucketAggs = append(q.BucketAggs, bucketAgg)
	bucketAgg.Settings = simplejson.NewFromAny(
		bucketAgg.generateSettingsForDSL(),
	)
	_ = addDateHistogramAgg(aggBuilder, bucketAgg, from, to, defaultTimeField)
}

// processDocumentQuery processes a document query (raw_data or raw_document) and configures the search request
func processDocumentQuery(q *Query, b *es.SearchRequestBuilder, from, to int64, defaultTimeField string) {
	metric := q.Metrics[0]
	b.Sort(es.SortOrderDesc, defaultTimeField, "boolean")
	b.Sort(es.SortOrderDesc, "_doc", "")
	b.AddDocValueField(defaultTimeField)
	if isRawDataQuery(q) {
		// For raw_data queries we need to add timeField as field with standardized time format to not receive
		// invalid formats that elasticsearch can parse, but our frontend can't (e.g. yyyy_MM_dd_HH_mm_ss)
		b.AddTimeFieldWithStandardizedFormat(defaultTimeField)
	}
	b.Size(stringToIntWithDefaultValue(metric.Settings.Get("size").MustString(), defaultSize))

	if q.IncludeRuntimeFields {
		b.EnableRuntimeFields()
	}
}

// processTimeSeriesQuery processes a time series query with aggregations and metrics
func processTimeSeriesQuery(q *Query, b *es.SearchRequestBuilder, from, to int64, defaultTimeField string) {
	aggBuilder := b.Agg()
	// Process buckets
	// iterate backwards to create aggregations bottom-down
	for _, bucketAgg := range q.BucketAggs {
		bucketAgg.Settings = simplejson.NewFromAny(
			bucketAgg.generateSettingsForDSL(),
		)
		switch bucketAgg.Type {
		case dateHistType:
			aggBuilder = addDateHistogramAgg(aggBuilder, bucketAgg, from, to, defaultTimeField)
		case histogramType:
			aggBuilder = addHistogramAgg(aggBuilder, bucketAgg)
		case filtersType:
			aggBuilder = addFiltersAgg(aggBuilder, bucketAgg)
		case termsType:
			aggBuilder = addTermsAgg(aggBuilder, bucketAgg, q.Metrics)
		case geohashGridType:
			aggBuilder = addGeoHashGridAgg(aggBuilder, bucketAgg)
		case nestedType:
			aggBuilder = addNestedAgg(aggBuilder, bucketAgg)
		}
	}

	// Process metrics
	for _, m := range q.Metrics {
		m := m

		if m.Type == countType {
			continue
		}

		// Saved queries can carry aggregation types that no longer exist
		// (moving_avg was removed in Elasticsearch 8.0). Skip them rather
		// than emit an aggregation Elasticsearch would reject.
		if _, known := metricAggType[m.Type]; !known {
			continue
		}

		if isPipelineAgg(m.Type) {
			if isPipelineAggWithMultipleBucketPaths(m.Type) {
				if len(m.PipelineVariables) > 0 {
					bucketPaths := make(map[string]any, len(m.PipelineVariables))
					for name, pipelineAgg := range m.PipelineVariables {
						var appliedAgg *MetricAgg
						for _, pipelineMetric := range q.Metrics {
							if pipelineMetric.ID == pipelineAgg {
								appliedAgg = pipelineMetric
								break
							}
						}
						if appliedAgg != nil {
							// The reference resolves to a sibling metric. This covers
							// numeric builder-UI IDs and named raw-DSL aggregation IDs.
							if appliedAgg.Type == countType {
								bucketPaths[name] = "_count"
							} else {
								bucketPaths[name] = pipelineAgg
							}
						} else if _, err := strconv.Atoi(pipelineAgg); err != nil {
							// A non-numeric reference that matches no sibling comes from a
							// raw-DSL query (e.g. a nested path like "agg>metric" or
							// "_count"): pass it through verbatim so Elasticsearch
							// validates it. Numeric references that match no sibling are
							// builder-UI pointers to deleted metrics and are dropped.
							bucketPaths[name] = pipelineAgg
						}
					}

					// Skip emitting the pipeline aggregation when no variable resolved
					// to a real metric or a raw-DSL reference: an empty buckets_path
					// would produce an invalid Elasticsearch query.
					if len(bucketPaths) == 0 {
						continue
					}

					aggBuilder.Pipeline(m.ID, m.Type, bucketPaths, func(a *es.PipelineAggregation) {
						a.Settings = m.generateSettingsForDSL()
					})
				} else {
					continue
				}
			} else {
				pipelineAggField := getPipelineAggField(m)
				if _, err := strconv.Atoi(pipelineAggField); err == nil {
					var appliedAgg *MetricAgg
					for _, pipelineMetric := range q.Metrics {
						if pipelineMetric.ID == pipelineAggField {
							appliedAgg = pipelineMetric
							break
						}
					}
					if appliedAgg != nil {
						bucketPath := pipelineAggField
						if appliedAgg.Type == countType {
							bucketPath = "_count"
						}

						aggBuilder.Pipeline(m.ID, m.Type, bucketPath, func(a *es.PipelineAggregation) {
							a.Settings = m.generateSettingsForDSL()
						})
					}
				} else {
					continue
				}
			}
		} else {
			aggBuilder.Metric(m.ID, m.Type, m.Field, func(a *es.MetricAggregation) {
				a.Settings = m.generateSettingsForDSL()
			})
		}
	}
}

func applyBoolFilters(bf *BoolFilterSet, b *es.SearchRequestBuilder) {
	filterBuilder := b.Query().Bool().Filter()
	for _, c := range bf.Filters {
		applySingleClause(c, filterBuilder)
	}
	mustNotBuilder := b.Query().Bool().MustNot()
	for _, c := range bf.MustNot {
		applySingleClause(c, mustNotBuilder)
	}
}

func applySingleClause(c BoolFilterClause, fb *es.FilterQueryBuilder) {
	switch c.Type {
	case "term":
		fb.AddTermFilter(c.Field, c.Value)
	case "terms":
		fb.AddTermsFilter(c.Field, c.Values)
	case "match_phrase":
		fb.AddMatchPhraseFilter(c.Field, c.Value)
	case "range":
		fb.AddGenericRangeFilter(c.Field, c.Bounds)
	case "wildcard":
		if s, ok := c.Value.(string); ok {
			fb.AddWildcardFilter(c.Field, s)
		}
	}
}

func (e *elasticsearchDataQuery) processRawDSLQuery(q *Query, b *es.SearchRequestBuilder) error {
	if q.RawQuery == "" {
		return backend.DownstreamError(fmt.Errorf("raw DSL query is empty"))
	}

	// Parse the raw DSL query JSON
	var queryBody map[string]any
	if err := json.Unmarshal([]byte(q.RawQuery), &queryBody); err != nil {
		return backend.DownstreamError(fmt.Errorf("invalid raw DSL query JSON: %w", err))
	}

	if len(q.Metrics) > 0 {
		firstMetricType := q.Metrics[0].Type
		if firstMetricType != logsType && firstMetricType != rawDataType && firstMetricType != rawDocumentType {
			bucketAggs, metricAggs, err := e.aggregationParserDSLRawQuery.Parse(q.RawQuery)
			if err != nil {
				return backend.DownstreamError(fmt.Errorf("failed to parse aggregations: %w", err))
			}

			// Only adopt the aggregations parsed from the raw body when it actually defines its
			// own. When it doesn't, keep the aggregations the frontend already supplied: this is
			// the case for the Explore logs-volume supplementary query, which carries a
			// Grafana-synthesized date_histogram (and optional per-level terms agg) in BucketAggs
			// even though the raw DSL body only contains a `query` clause. Overwriting them here
			// is what made the logs view fail to load log volume in the raw query editor.
			// See https://github.com/grafana/grafana-elasticsearch-datasource/issues/112
			if len(bucketAggs) > 0 || len(metricAggs) > 0 {
				// If there is no metric agg in the query, it is a count agg
				if len(metricAggs) == 0 {
					metricAggs = append(metricAggs, &MetricAgg{Type: "count"})
				}

				q.BucketAggs = bucketAggs
				q.Metrics = metricAggs
			}

			// Apply the user's `query` clause as a bool filter so the aggregation respects it.
			// The date-range filter has already been added by the caller, and Bool()/Filter()
			// reuse that same filter builder.
			if queryPart, ok := queryBody["query"].(map[string]any); ok {
				b.Query().Bool().Filter().AddRawFilter(queryPart)
			}
			return nil
		}
	}

	// For non-time-series queries (logs, raw data), pass through the raw body directly
	b.SetRawBody(queryBody)
	return nil
}

// getPipelineAggField returns the pipeline aggregation field
func getPipelineAggField(m *MetricAgg) string {
	// In frontend we are using Field as pipelineAggField
	// There might be historical reason why in backend we were using PipelineAggregate as pipelineAggField
	// So for now let's check Field first and then PipelineAggregate to ensure that we are not breaking anything
	// TODO: Investigate, if we can remove check for PipelineAggregate
	pipelineAggField := m.Field

	if pipelineAggField == "" {
		pipelineAggField = m.PipelineAggregate
	}
	return pipelineAggField
}
