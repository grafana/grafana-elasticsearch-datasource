# Grafana dataplane LogLines compliance

Living planning doc for the dataplane logs-frame migration. Tracks scope, state, and follow-ups. Update alongside each change that lands a chunk of the work.

- Tracking issue: [grafana/grafana#71757](https://github.com/grafana/grafana/issues/71757)
- Loki precedent (behind feature flag): [grafana/grafana#69909](https://github.com/grafana/grafana/pull/69909)
- Spec: [grafana/dataplane LogLines contract](https://github.com/grafana/dataplane/blob/main/docs/contract/logs.md)
- Doc opened: 2026-06-17.

## Why

Grafana's dataplane contract standardises the shape of log dataframes returned by data sources (`timestamp`/`body`/`severity`/`id`/`labels`/`labelTypes`, `Meta.Type = log-lines`). Adopting it gives the Elasticsearch data source consistent treatment in Explore, Logs panel, transformations, and downstream consumers (alerting, public-dashboards, server-side expressions) — and unblocks future features that depend on the contract (Log Details label grouping, OTel attribute interop).

The migration is a **breaking change** to the on-the-wire frame layout, so it ships behind the `elasticsearchLogsDataplane` feature toggle. The legacy shape remains the default until the toggle defaults to on (matching Loki's rollout cadence).

## Scope

| Concern | Status |
|--------|--------|
| Feature toggle (`elasticsearchLogsDataplane`) | Done — gated in [pkg/elasticsearch/data_query.go](pkg/elasticsearch/data_query.go) |
| Canonical fields (timestamp/body/severity/id/labels) prepended | Done — [pkg/elasticsearch/dataplane.go](pkg/elasticsearch/dataplane.go) |
| Frame `Meta.Type = log-lines`, `TypeVersion = {0,0}` | Done — `setLogLinesFrameMeta` |
| Unique `id` from `_index#_id` | Done — reuses existing field from [pkg/elasticsearch/logs_response_processor.go](pkg/elasticsearch/logs_response_processor.go) |
| `labelTypes` field (Field/Metadata/ArrayField) | Done — keys from `hit["fields"]` tagged Metadata; arrays tagged ArrayField; everything else Field |
| ES\|QL logs query coverage | Done — `processEsqlLogsResponse` (all keys are Field-category) |
| Body fallback removed when `LogMessageField` unset | Done — `body` stays `""` rather than serialising the whole doc |
| Unit tests for toggle on/off, labelTypes categorisation, body fallback | Done — [pkg/elasticsearch/dataplane_test.go](pkg/elasticsearch/dataplane_test.go) |

## Severity values

`severity` is a pass-through of the configured `LogLevelField` value. The spec expects [Grafana's level enum](https://grafana.com/docs/grafana/latest/explore/logs-integration/) (`critical`/`error`/`warning`/`info`/`debug`/`trace`); the value must already conform via data-source config. The plugin does **not** normalise (e.g. mapping `warn` → `warning`) — that's the user's responsibility on the index side. If we see pain reports we can revisit, but matching upstream behaviour is the safer default.

## `labelTypes` and the frontend

The spec notes:

> data source must fulfill the `DataSourceWithLogsLabelTypesSupport` interface, otherwise all labels will display in the default "Fields" category

This is a frontend (TypeScript) requirement. We emit `labelTypes` server-side so the data is available, but the data-source class on the frontend doesn't yet implement the interface. Until it does, consumers treat every label as Field-category. Tracked as a follow-up:

- [ ] Frontend: implement `DataSourceWithLogsLabelTypesSupport` on `ElasticDatasource` so Log Details renders `Metadata` and `ArrayField` separately (Grafana 12.4+).

## Rollout

1. Land this PR with toggle off by default.
2. Verify in Explore against a sample dataset (golden path + level field + array fields).
3. Watch for downstream consumer breakage (alerting, SSE, public dashboards) on the legacy shape vs LogLines shape. Loki's experience suggests minimal fallout; consumers branch on `Meta.Type` already.
4. Flip the toggle default once Grafana 12.x consumers have adopted, mirroring Loki.

## Sequencing notes

- Open PRs that should land first: [#312](https://github.com/grafana/grafana-elasticsearch-datasource/pull/312) (bump `grafanaDependency` floor to ≥12.2.0) and [#302](https://github.com/grafana/grafana-elasticsearch-datasource/pull/302) (`grafana-plugin-sdk-go` v0.292.1). The toggle wiring requires `data.FrameTypeLogLines` from the SDK; v0.291.x in `go.mod` is already sufficient, but staying current avoids churn.
- [#273](https://github.com/grafana/grafana-elasticsearch-datasource/pull/273) (`go-elasticsearch` v8 swap) is expected to merge much later. It rewrites `pkg/elasticsearch/client/` and rebases its `ConfiguredFields` plumbing. The dataplane work reads `ConfiguredFields.{TimeField,LogMessageField,LogLevelField}` via the same interface, so conflicts will be textual (`data_query.go`, `parseResponse` signature) and resolvable on rebase rather than semantic.
- [#315](https://github.com/grafana/grafana-elasticsearch-datasource/pull/315) (per-query index selection) may introduce new doc keys. They'll flow into `labels` as `Field`-category unless they collide with the configured time/message/level fields; no action needed.

## Out of scope (for now)

- Dataplane compliance for **metrics** frames. Different spec (`numeric-wide` etc.) and bigger blast radius. Track separately with a follow-up issue and a sibling toggle (`elasticsearchMetricsDataplane`).
- Raw-data (table) responses. Not logs-shaped; spec doesn't apply.
- Frontend rendering work beyond the `DataSourceWithLogsLabelTypesSupport` hook called out above.
