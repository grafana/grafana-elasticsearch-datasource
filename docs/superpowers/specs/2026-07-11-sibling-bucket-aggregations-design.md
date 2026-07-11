# Sibling bucket aggregations (Sum of Max, Max of Max) — design

Date: 2026-07-11
Status: approved

## Problem

The datasource cannot express composite peak metrics such as "Sum of Max" (sum, per
time bucket, of each host's maximum) or "Max of Max" (overall peak across hosts per
time bucket). These are critical for capacity and storage-utilisation dashboards
across large estates. In Elasticsearch these are sibling pipeline aggregations
(`sum_bucket`, `max_bucket`) over a `terms` aggregation containing a metric, but the
datasource only supports parent pipeline aggregations (derivative, cumulative sum,
moving function, serial difference, bucket script). Users currently export data or
build workarounds outside Grafana.

## Decisions taken

- Scope: a generic outer-by-inner composite covering the whole useful family
  (sum/max/min/avg outer over max/min/sum/avg inner), not just the two named
  aggregations.
- Modelling: one new metric type per outer operation, named after the ES sibling
  pipeline aggregation each emits.
- Default hidden-terms `size`: 500, user-editable, capped at ES's 65535.

## Approach

Each new metric type is a self-contained composite. At query-build time it emits,
at the innermost bucket level:

1. a hidden `terms` aggregation on the group-by field (size = limit), containing
   the inner stat aggregation on the value field, and
2. the ES sibling pipeline aggregation (`sum_bucket` etc.) whose `buckets_path`
   points at `<hidden terms id>><inner stat id>`.

The composite's value appears in the response at `bucket[metricId].value` in every
date_histogram bucket, exactly where the existing response parser reads single-value
metrics. The parser only recurses into bucket aggregations listed in the query
model, so the hidden terms aggregation is ignored without any parser changes. The
linear bucket-agg model, trim logic, and alerting path are untouched.

Composites nest naturally under visible bucket aggregations: with
`[terms(cluster), date_histogram]` the composite is emitted inside each cluster's
date_histogram, yielding per-cluster Sum of Max series with no extra code.

### Alternatives rejected

- Raw sibling pipeline aggregations with user-managed nesting: breaks the
  "metrics always innermost" invariant the response parser is built on, and
  reproduces the ES-internals UX the feature is meant to remove.
- Client-side reduction via transformations: ships thousands of per-host series
  over the wire on large estates and does not work for alerting.

## Query model

Four new metric types: `sum_bucket`, `max_bucket`, `min_bucket`, `avg_bucket`.
Display names follow Kibana: "Sum Bucket", "Max Bucket", "Min Bucket",
"Average Bucket".

Each is a `MetricAggregationWithField` (the numeric value field) with settings:

| Setting   | Meaning                                    | Default | Constraints        |
| --------- | ------------------------------------------ | ------- | ------------------ |
| `metric`  | inner stat: `max`, `min`, `sum`, `avg`     | `max`   | required           |
| `groupBy` | keyword field for the hidden terms agg     | —       | required           |
| `limit`   | hidden terms `size`                        | `500`   | 1–65535            |

"Sum of Max" = Sum Bucket with inner `max`. "Max of Max" = Max Bucket with inner
`max`. "Sum of Sums" = Sum Bucket with inner `sum`.

Types are added to both hand-maintained schema twins: `src/dataquery.gen.ts` and
`pkg/elasticsearch/kinds/dataquery/types_dataquery_gen.go`.

### Classification

The new types are NOT added to the existing `pipelineAggType` maps
(`pkg/elasticsearch/models.go`, `src/.../MetricAggregationsEditor/utils.ts`): that
machinery assumes `buckets_path` references another metric row by ID and drives the
"apply to metric" picker, neither of which applies. Instead a new sibling
classification (`isSiblingPipelineAgg` in Go, a config flag in the TS
`metricAggregationConfig`) gates the new code paths. Sibling composites are
explicitly filtered out of the single-buckets-path "apply to" pickers and out of
terms order-by targets (the pre-existing filters only excluded
pipeline-classified types). The bucket_script variable picker intentionally
keeps its full metric list, since chained pipeline references were already
permitted there.

## Backend query generation

All metric queries execute through the Go backend, so
`processTimeSeriesQuery` in `pkg/elasticsearch/data_query_processor.go` is the
authoritative path. Its metric loop gains a branch for sibling types using the
existing builder API:

```go
aggBuilder.Terms(m.ID+"_groupby", groupBy, func(a *es.TermsAggregation, b es.AggBuilder) {
    a.Size = limit
    b.Metric(m.ID+"_inner", innerStat, m.Field, nil)
})
aggBuilder.Pipeline(m.ID, m.Type, m.ID+"_groupby>"+m.ID+"_inner", ...)
```

If `groupBy` is empty the metric is skipped entirely, mirroring the existing
skip-empty-`buckets_path` guard for bucket scripts. The frontend
`src/QueryBuilder.ts` gets the mirrored logic to stay in parity with the backend,
as existing maintenance does.

## Response parsing and naming

No structural parser changes. The composite routes through the default
single-value metric path (`processDefaultMetric`), which is already null-safe for
empty buckets. Series naming:

- Go `pkg/elasticsearch/field_namer.go`: `<Outer> of <Inner> <field> per <groupBy>`
  (e.g. "Sum of Max storage_used per host").
- TS `src/components/QueryEditor/MetricAggregationsEditor/SettingsEditor/useDescription.ts`
  for the editor row summary.
- Display-name entries added to `metricAggType` in `pkg/elasticsearch/models.go`.

## UI

One new settings editor panel (in
`src/components/QueryEditor/MetricAggregationsEditor/SettingsEditor/`) shared by
the four types:

- inner-stat select (Max/Min/Sum/Average),
- group-by field picker sourced from keyword fields (same source as the terms
  bucket aggregation editor),
- limit input with `500` placeholder.

The metric's main field picker remains the standard numeric field picker. An
inline validation hint appears while group-by is unset.

## Errors and edge cases

- Missing group-by: metric not emitted by the backend, inline hint in the UI.
- Empty date_histogram buckets: ES returns `null`, existing null handling applies.
- `limit` above 65535: clamped to 65535.
- Truncation: a `limit` smaller than the estate's cardinality silently undercounts
  sums. Documented in the plugin docs alongside the cost trade-off.

## Testing

- Go: DSL-emission tests in `data_query_test.go` plus snapshot fixtures,
  response-parsing tests with a realistic sibling-agg response fixture in
  `response_parser_test.go`, field-naming tests.
- TS: `QueryBuilder.test.ts` parity tests, `queryDef.test.ts`, settings-editor
  component test.
- E2E: one scenario covering Sum of Max end-to-end against the docker ES stack,
  verified on both Elasticsearch 8.x and 9.x.
- Alerting works unchanged because the whole path is backend-side.

## Out of scope

- `stats_bucket` and `percentiles_bucket`.
- Multi-level group-bys inside the composite.
- Exposing raw sibling pipeline aggregations.
- `count` as an inner stat.
