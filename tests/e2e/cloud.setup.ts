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

setup('provision cloud datasources', async ({ request }) => {
  setup.skip(!isCloudRun, 'Local and PR CI use provisioning/datasources/datasources.yml.');

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
});
