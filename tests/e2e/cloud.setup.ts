import { expect, test as setup } from '@grafana/plugin-e2e';
import { type APIRequestContext } from '@playwright/test';

import {
  CLOUD_LOGS_INDEX,
  CLOUD_LOGS_NAME,
  CLOUD_LOGS_UID,
  CLOUD_MANAGED_UID,
  CLOUD_METRICS_INDEX,
  CLOUD_METRICS_NAME,
  CLOUD_METRICS_UID,
  isCloudRun,
  transportErrorFor,
} from './testEnv';

// The shared Cloud instance never applies provisioning/datasources/datasources.yml, and the
// datasource the data-sources Pulumi project provisions there configures no index, so no
// existing datasource can answer the queries these specs make. This setup project creates the
// two the suite needs, copying the connection URL and Private Data Source Connect settings from
// the managed datasource so the traffic takes the same network path.
//
// Create-or-update against fixed UIDs, not unique ones: reruns then reuse the same two
// datasources instead of leaving a new pair behind on a shared instance every night. Nothing
// here is destructive to anything the suite does not own.

interface ManagedDataSource {
  url: string;
  jsonData?: Record<string, unknown>;
}

async function readManagedDataSource(request: APIRequestContext): Promise<ManagedDataSource> {
  const resp = await request.get(`/api/datasources/uid/${CLOUD_MANAGED_UID}`);
  expect(
    resp.ok(),
    `Managed datasource ${CLOUD_MANAGED_UID} is not available on the Cloud instance (status ${resp.status()}). ` +
      'Set DS_E2E_MANAGED_UID if the provisioned Elasticsearch datasource was renamed.'
  ).toBe(true);
  return (await resp.json()) as ManagedDataSource;
}

async function upsertDataSource(
  request: APIRequestContext,
  managed: ManagedDataSource,
  spec: { uid: string; name: string; index: string; logLevelField?: string; logMessageField?: string }
): Promise<void> {
  // Carry over enableSecureSocksProxy / secureSocksProxyUsername so the request reaches the
  // backend through the same PDC tunnel the managed datasource uses.
  const body = {
    uid: spec.uid,
    name: spec.name,
    type: 'elasticsearch',
    access: 'proxy',
    url: managed.url,
    database: spec.index,
    jsonData: {
      ...managed.jsonData,
      index: spec.index,
      timeField: '@timestamp',
      ...(spec.logMessageField ? { logMessageField: spec.logMessageField } : {}),
      ...(spec.logLevelField ? { logLevelField: spec.logLevelField } : {}),
    },
  };

  const existing = await request.get(`/api/datasources/uid/${spec.uid}`);
  const resp = existing.ok()
    ? await request.put(`/api/datasources/uid/${spec.uid}`, { data: body })
    : await request.post('/api/datasources', { data: body });

  expect(resp.ok(), `Failed to provision ${spec.name}: ${resp.status()} ${await resp.text()}`).toBe(true);

  // Fail here rather than let every downstream spec time out on an unreachable backend.
  const health = await request.get(`/api/datasources/uid/${spec.uid}/health`);
  const healthBody = (await health.json().catch(() => ({}))) as { status?: string; message?: string };
  expect(
    healthBody.status,
    `${spec.name} is not healthy: ${healthBody.message ?? `HTTP ${health.status()}`}`
  ).toBe('OK');
}

// The health check above and query traffic do not share a connection, so health reports OK while
// the run's first query still pays a cold Private Data Source Connect dial — and that dial fails
// outright ("socks connect tcp ... host unreachable") instead of waiting. It broke whichever spec
// queried first (the ES|QL macro tests) on roughly 40% of nightly runs, and Playwright's retries
// re-ran them inside the same two-second window, so all three attempts failed together. Dial the
// query path here instead, with backoff, so the tunnel is established before any spec queries.
async function waitForQueryPath(
  request: APIRequestContext,
  uid: string,
  query: Record<string, unknown>
): Promise<void> {
  const post = () =>
    request.post('/api/ds/query', {
      data: {
        from: String(Date.now() - 60_000),
        to: String(Date.now()),
        queries: [
          {
            refId: 'A',
            datasource: { type: 'elasticsearch', uid },
            intervalMs: 60_000,
            maxDataPoints: 10,
            ...query,
          },
        ],
      },
    });

  // Only a transport error is retried. A query Elasticsearch answers — even to reject — means the
  // tunnel is up, which is all this gate is for. A request that throws (socket reset, the request
  // context's own timeout) is polled the same way; expect.poll would otherwise abort on it.
  await expect
    .poll(async () => post().then(transportErrorFor, (error: unknown) => String(error)), {
      message: `Query traffic to ${uid} never got through Private Data Source Connect`,
      intervals: [1_000, 2_000, 4_000, 8_000, 15_000],
      timeout: 60_000,
    })
    .toBeNull();
}

const LUCENE_COUNT = {
  queryType: 'lucene',
  query: '*',
  metrics: [{ type: 'count', id: '1' }],
  bucketAggs: [{ type: 'date_histogram', id: '2', field: '@timestamp', settings: { interval: 'auto' } }],
  timeField: '@timestamp',
};

setup('provision cloud datasources', async ({ request }) => {
  setup.skip(!isCloudRun, 'Local and PR CI use provisioning/datasources/datasources.yml.');
  // The query-path gates below run concurrently and each polls for up to 60 s. The config's 90 s
  // Cloud timeout leaves too little room for provisioning plus a gate's final in-flight request,
  // and running out would skip the whole chromium project on a generic timeout.
  setup.setTimeout(150_000);

  const managed = await readManagedDataSource(request);

  await upsertDataSource(request, managed, {
    uid: CLOUD_LOGS_UID,
    name: CLOUD_LOGS_NAME,
    index: CLOUD_LOGS_INDEX,
    logMessageField: 'message',
    // The datagen mapping makes `level` a text field with a `.keyword` subfield; the log-volume
    // terms aggregation needs the keyword form.
    logLevelField: 'level.keyword',
  });

  await upsertDataSource(request, managed, {
    uid: CLOUD_METRICS_UID,
    name: CLOUD_METRICS_NAME,
    index: CLOUD_METRICS_INDEX,
  });

  // Each datasource instance holds its own HTTP client, and Lucene (_msearch) and ES|QL (_query)
  // are separate endpoints on it, so every combination the suite uses gets dialled. The ES|QL
  // probe names the index literally: the macro that resolves it is what macros.spec.ts covers,
  // and a gate that depended on it would report a macro bug as unreachable infrastructure.
  // Concurrent, so a slow first dial on one path cannot eat the others' budget.
  await Promise.all([
    waitForQueryPath(request, CLOUD_LOGS_UID, LUCENE_COUNT),
    waitForQueryPath(request, CLOUD_METRICS_UID, LUCENE_COUNT),
    waitForQueryPath(request, CLOUD_LOGS_UID, {
      queryType: 'esql',
      query: `FROM ${CLOUD_LOGS_INDEX} | STATS c = COUNT(*)`,
    }),
  ]);
});
