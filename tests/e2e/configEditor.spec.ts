import { expect, test } from '@grafana/plugin-e2e';
import { Page } from '@playwright/test';

import { ElasticsearchOptions } from '../src/types';
import { healthPathFor, isCloudRun } from './testEnv';

const PLUGIN_TYPE = 'elasticsearch';

// Resolves the Elasticsearch URL for an ad-hoc datasource health check: the injected Cloud
// URL when present, else the docker-compose backend in CI, else localhost.
function resolveElasticsearchUrl(env = process.env) {
  return env.CI ? env.DS_INSTANCE_URL || 'http://elasticsearch:9200' : 'http://localhost:9200';
}

// Selects a Private Data Source Connect network in the datasource config editor. The
// combobox is a Grafana-core element, present only when PDC is available on the instance
// (i.e. the shared Cloud instance), so it is called only when a network name is injected.
async function configurePDC(page: Page, networkName: string) {
  await page.getByRole('combobox', { name: 'Private data source connect' }).click();
  await page.getByText(networkName).click();
}

test.describe('Config editor', () => {
  test.describe('rendering', () => {
    test(
      'smoke: should render config editor',
      { tag: '@plugins' },
      async ({ createDataSourceConfigPage, page }) => {
        await createDataSourceConfigPage({ type: PLUGIN_TYPE });
      }
    );

    test('should render Elasticsearch details section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const esSection = page.getByText('Elasticsearch details').first();
      await esSection.scrollIntoViewIfNeeded();
      await expect(esSection).toBeVisible();

      await expect(page.getByLabel('Index name')).toBeVisible();
      await expect(page.getByLabel('Time field name')).toBeVisible();
      await expect(page.getByLabel('Max concurrent Shard Requests')).toBeVisible();
      await expect(page.getByLabel('Min time interval')).toBeVisible();
      await expect(page.getByLabel('Include Frozen Indices')).toBeVisible();
    });

    test('should render Logs section', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const logsSection = page.getByRole('heading', { name: 'Logs', exact: true });
      await logsSection.scrollIntoViewIfNeeded();
      await expect(logsSection).toBeVisible();

      await expect(page.getByLabel('Message field name')).toBeVisible();
      await expect(page.getByLabel('Level field name')).toBeVisible();
    });

    test('should render Data links section with Add button', async ({ createDataSourceConfigPage, page }) => {
      await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      const dataLinksSection = page.getByRole('heading', { name: 'Data links', exact: true });
      await dataLinksSection.scrollIntoViewIfNeeded();
      await expect(dataLinksSection).toBeVisible();

      // Multiple 'Add' buttons exist on the page (e.g. Allowed cookies); the Data links one is last
      await expect(page.getByRole('button', { name: 'Add', exact: true }).last()).toBeVisible();
    });
  });

  test.describe('provisioned datasource', () => {
    // The shared Cloud instance doesn't apply the local provisioning/datasources/datasources.yml,
    // so these assertions of provisioned values can't run there (grafana/clickhouse-datasource#1934).
    test.beforeEach(() => {
      test.skip(isCloudRun, 'Provisioned-datasource assertions require local provisioning, not applied on Cloud.');
    });

    test('should load provisioned Elasticsearch details', async ({
      readProvisionedDataSource,
      gotoDataSourceConfigPage,
      page,
    }) => {
      const ds = await readProvisionedDataSource<ElasticsearchOptions>({ fileName: 'datasources.yml' });
      await gotoDataSourceConfigPage(ds.uid);

      await page.getByText('Elasticsearch details').first().scrollIntoViewIfNeeded();
      await expect(page.getByLabel('Index name')).toHaveValue(ds.jsonData.index!);
      await expect(page.getByLabel('Time field name')).toHaveValue(ds.jsonData.timeField);
    });

    test('should load provisioned Logs fields', async ({
      readProvisionedDataSource,
      gotoDataSourceConfigPage,
      page,
    }) => {
      const ds = await readProvisionedDataSource<ElasticsearchOptions>({ fileName: 'datasources.yml' });
      await gotoDataSourceConfigPage(ds.uid);

      await page.getByRole('heading', { name: 'Logs', exact: true }).scrollIntoViewIfNeeded();
      await expect(page.getByLabel('Message field name')).toHaveValue(ds.jsonData.logMessageField!);
      await expect(page.getByLabel('Level field name')).toHaveValue(ds.jsonData.logLevelField!);
    });
  });

  test.describe('save & test', () => {
    test('should pass health check for provisioned datasource', async ({
      readProvisionedDataSource,
      gotoDataSourceConfigPage,
    }) => {
      test.skip(isCloudRun, 'Health-checks the locally-provisioned datasource, not applied on Cloud.');
      const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
      const configPage = await gotoDataSourceConfigPage(ds.uid);

      // toBeOK() takes a Promise<Response> — pass the unawaited call
      await expect(configPage.saveAndTest({ path: healthPathFor(ds.uid) })).toBeOK();
      await expect(configPage).toHaveAlert('success');
    });

    test('should show error alert when health check fails', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      // A URL must be present for the save to succeed and trigger the health check.
      // The mock then intercepts /health and returns a 400 so we can assert the error UI.
      await page.getByLabel('Data source connection URL').fill('http://elasticsearch:9200');
      await configPage.mockHealthCheckResponse({ message: 'Failed to connect to Elasticsearch', status: 'ERROR' }, 400);

      await configPage.saveAndTest({ path: healthPathFor(configPage.datasource.uid) });
      await expect(configPage).toHaveAlert('error');
    });

    test('should show error alert when Elasticsearch is unreachable', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      // Point at a port nothing is listening on — the backend health check will fail for real
      await page.getByLabel('Data source connection URL').fill('http://localhost:19200');
      await configPage.saveAndTest({ path: healthPathFor(configPage.datasource.uid) });
      await expect(configPage).toHaveAlert('error');
    });

    test('valid credentials should pass the health check', async ({ createDataSourceConfigPage, page }) => {
      // Ad-hoc datasource health check against a reachable backend: the docker-compose
      // Elasticsearch in local/PR CI, or DS_INSTANCE_URL when injected by the Cloud cron
      // (playwright-cloud) via Vault repo-secrets. Requires a reachable backend, so skipped
      // when neither CI nor DS_INSTANCE_URL is set.
      test.skip(
        !process.env.CI && !process.env.DS_INSTANCE_URL,
        'Elasticsearch must be reachable; set DS_INSTANCE_URL or run in CI'
      );
      // Verified against the Cloud instance: an ad-hoc datasource pointed at the managed backend
      // health-checks inconsistently — HTTP 400 on one attempt, no health response at all on the
      // next, while the managed datasource itself stays healthy. Specific to configuring PDC on a
      // datasource created mid-test (grafana/clickhouse-datasource#1934).
      test.skip(
        isCloudRun,
        'Ad-hoc save & test against the managed Cloud backend is unreliable; covered by local/PR CI.'
      );
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });
      await page.getByLabel('Data source connection URL').fill(resolveElasticsearchUrl());

      if (process.env.DS_PDC_NETWORK_NAME) {
        await configurePDC(page, process.env.DS_PDC_NETWORK_NAME);
      }

      await expect(configPage.saveAndTest({ path: healthPathFor(configPage.datasource.uid) })).toBeOK();
      await expect(configPage).toHaveAlert('success');
    });
  });
});
