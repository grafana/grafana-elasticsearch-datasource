import { type Page } from '@playwright/test';

// GRAFANA_URL is set only by the Cloud cron workflow (playwright-cloud); its presence signals
// a run against the shared Cloud instance rather than local/PR CI.
export const isCloudRun = !!process.env.GRAFANA_URL;

// The Cloud instance does not apply provisioning/datasources/datasources.yml, and its
// Elasticsearch backend is reachable only through Private Data Source Connect. It also holds
// different indices from the local fixtures: a datagen sidecar keeps `grafana-logs` and
// `grafana-metrics` filled with recent documents. Cloud runs therefore target datasources this
// suite creates itself (tests/e2e/cloud.setup.ts) over those two indices, with the field names
// and time window below swapped to match.
//
// The datasource proxy is read-only for writes ("posts not allowed on proxied Elasticsearch
// datasource except on /_msearch"), so the local fixture indices cannot be loaded onto Cloud.
// Assertions that depend on exact fixture contents relax to shape-only checks there.

// UIDs of the datasources cloud.setup.ts creates. Fixed rather than generated so reruns reuse
// them instead of littering the shared instance, and so specs can resolve a UID without waiting
// on setup output.
export const CLOUD_LOGS_UID = process.env.DS_E2E_LOGS_UID || 'es-e2e-logs';
export const CLOUD_METRICS_UID = process.env.DS_E2E_METRICS_UID || 'es-e2e-metrics';
export const CLOUD_LOGS_NAME = 'es-e2e-logs';
export const CLOUD_METRICS_NAME = 'es-e2e-metrics';

// The managed datasource cloud.setup.ts copies its URL and PDC settings from. Provisioned by
// the data-sources Pulumi project; override if the ES version behind the lane changes.
export const CLOUD_MANAGED_UID = process.env.DS_E2E_MANAGED_UID || 'elasticsearch9_1-ds-m';

export const CLOUD_LOGS_INDEX = process.env.DS_E2E_LOGS_INDEX || 'grafana-logs';
export const CLOUD_METRICS_INDEX = process.env.DS_E2E_METRICS_INDEX || 'grafana-metrics';

const LOCAL_FIXTURE_FROM_ISO = '2026-03-17T21:00:00.000Z';
const LOCAL_FIXTURE_TO_ISO = '2026-03-18T01:00:00.000Z';

// Cloud datagen writes continuously, so a window relative to run start always holds documents.
// Resolved once per run so every spec in a run asserts over the same range.
const CLOUD_WINDOW_MS = 6 * 60 * 60 * 1000;
const cloudTo = new Date();
const cloudFrom = new Date(cloudTo.getTime() - CLOUD_WINDOW_MS);

interface LogsFields {
  /** Keyword field with a small set of repeated values — safe for terms aggregations. */
  termsField: string;
  /** Keyword field with one distinct value per document, for max-bucket coverage. */
  highCardinalityField: string;
  /** Field every document carries, asserted on Raw Data frames. */
  rawDataField: string;
  /** A Lucene filter that matches a subset of documents. */
  luceneFilter: string;
  /** Field to group an ES|QL STATS by. */
  esqlGroupBy: string;
}

interface MetricsFields {
  luceneFilter: string;
  rawDataField: string;
  /** Numeric field a sum_bucket metric can be calculated on. */
  numericField: string;
  /** Keyword field the sum_bucket groups by. */
  groupByField: string;
}

interface TestEnv {
  /** Name of the datasource selected through the picker (Explore, variable editor). */
  defaultDatasourceName: string;
  /** UID of the same datasource, for pinning an Explore pane without using the picker. */
  defaultDatasourceUid: string;
  /** Name of the metrics datasource, for the picker-driven sum-bucket test. */
  metricsDatasourceName: string;
  /** Stands in for the local `httplogs` fixture index. */
  logsUid: string;
  /** Stands in for the local `infra` fixture index. */
  metricsUid: string;
  /** Datasource with logLevelField configured, for the log-volume regression spec. */
  appLogsUid: string;
  /** Datasource over an index with a boolean field, or null where no such index exists. */
  authEventsUid: string | null;
  /** Index the logs datasource points at, for $__index macro assertions. */
  logsIndex: string;
  from: string;
  to: string;
  /** Exact document count in the logs index, or null when the data is live and only shape holds. */
  logsDocCount: number | null;
  logs: LogsFields;
  metrics: MetricsFields;
}

const local: TestEnv = {
  defaultDatasourceName: 'elasticsearch',
  defaultDatasourceUid: 'elasticsearch-e2e',
  metricsDatasourceName: 'infra',
  logsUid: 'httplogs-e2e',
  metricsUid: 'infra-e2e',
  appLogsUid: 'app-logs-e2e',
  authEventsUid: 'auth-events-e2e',
  logsIndex: 'httplogs',
  from: LOCAL_FIXTURE_FROM_ISO,
  to: LOCAL_FIXTURE_TO_ISO,
  logsDocCount: 200,
  logs: {
    termsField: 'method.keyword',
    highCardinalityField: 'traceId.keyword',
    rawDataField: 'statusCode',
    luceneFilter: 'method:GET',
    esqlGroupBy: 'method',
  },
  metrics: {
    luceneFilter: 'role:web',
    rawDataField: 'host',
    numericField: 'cpu.usagePercent',
    groupByField: 'host.keyword',
  },
};

const cloud: TestEnv = {
  defaultDatasourceName: CLOUD_LOGS_NAME,
  defaultDatasourceUid: CLOUD_LOGS_UID,
  metricsDatasourceName: CLOUD_METRICS_NAME,
  logsUid: CLOUD_LOGS_UID,
  metricsUid: CLOUD_METRICS_UID,
  appLogsUid: CLOUD_LOGS_UID,
  authEventsUid: null,
  logsIndex: CLOUD_LOGS_INDEX,
  from: cloudFrom.toISOString(),
  to: cloudTo.toISOString(),
  logsDocCount: null,
  logs: {
    termsField: 'level.keyword',
    highCardinalityField: 'request_id.keyword',
    rawDataField: 'message',
    luceneFilter: 'level:INFO',
    esqlGroupBy: 'level',
  },
  metrics: {
    luceneFilter: 'app:grafana',
    rawDataField: 'host',
    numericField: 'value',
    groupByField: 'host.keyword',
  },
};

export const env: TestEnv = isCloudRun ? cloud : local;

// True when Grafana serves this repo's externalised build rather than the datasource bundled
// into Grafana itself. Some Grafana versions (observed: 11.6.x) load the in-tree datasource even
// with `as_external = true`, reporting `module: core:plugin/elasticsearch`; the specs that guard
// on this cover fixes that live only in this repo, so they skip rather than fail there.
//
// The module path varies by how the plugin is served: `public/plugins/...` when Grafana serves it
// locally, and an absolute https:// URL when a CDN does (as on the Cloud instance). Only the
// `core:` scheme means the in-tree build, so test for that rather than for a path prefix.
export async function externalPluginIsLoaded(page: Page): Promise<boolean> {
  const resp = await page.request.get('/api/plugins/elasticsearch/settings');
  if (!resp.ok()) {
    return false;
  }
  const settings = (await resp.json()) as { module?: string };
  return typeof settings.module === 'string' && !settings.module.startsWith('core:');
}

// Grafana 13.2.0 replaced the dashboard settings Variables tab with an edit-sidebar redesign,
// gated on `grafana.dashboardSettingsRedesign`. On the shared Cloud instance (13.3.0) that flag
// is baked into grafanaBootData server-side, so @grafana/plugin-e2e's OFREP override cannot turn
// it off, and its VariablePage / VariableEditPage models — which drive the old settings-tab UI —
// have nothing to click. Tests that edit dashboard variables through the UI therefore cannot run
// there. They still run in PR CI against the local Grafana (12.3.0), which keeps the tab.
export const DASHBOARD_VARIABLES_UI_UNAVAILABLE = isCloudRun;
export const DASHBOARD_VARIABLES_UI_SKIP_REASON =
  'Grafana 13 moved dashboard variable editing out of the settings tab that @grafana/plugin-e2e drives; covered by PR CI.';

// Grafana 13 routes query traffic from the browser through the query apiserver
// (/apis/query.grafana.app/<version>/namespaces/<ns>/query) rather than /api/ds/query. The
// request and response bodies are unchanged, so tests that wait on a query only need to
// recognise both URLs. Older Grafana versions, and any direct API call a spec makes itself,
// still use the legacy path.
const APISERVER_QUERY_PATH = /\/apis\/query\.grafana\.app\/[^/]+\/namespaces\/[^/]+\/query(\?|$)/;

export function isQueryRequestUrl(url: string): boolean {
  return url.includes('/api/ds/query') || APISERVER_QUERY_PATH.test(url);
}

// Grafana 13 saves and health-checks datasources through the apiserver route
// (/apis/<group>/<version>/namespaces/<ns>/datasources/<uid>/health) while @grafana/plugin-e2e
// waits on the legacy /api/datasources/uid/<uid>/health. Matching on the uid-scoped suffix
// resolves against both, so saveAndTest() returns instead of timing out.
export function healthPathFor(uid: string): string {
  return `${uid}/health`;
}

