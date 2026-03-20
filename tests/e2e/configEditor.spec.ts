import { expect, test } from '@grafana/plugin-e2e';

import { ElasticsearchOptions } from '../src/types';

const PLUGIN_TYPE = 'elasticsearch';

test.describe('Config editor', () => {
  test.describe('rendering', () => {
    test(
      'smoke: should render config editor',
      { tag: '@plugins' },
      async ({ createDataSourceConfigPage, page }) => {
        await createDataSourceConfigPage({ type: PLUGIN_TYPE });

        await expect(page.getByText('Type: Elasticsearch', { exact: true })).toBeVisible();
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
      const ds = await readProvisionedDataSource({ fileName: 'datasources.yml' });
      const configPage = await gotoDataSourceConfigPage(ds.uid);

      // toBeOK() takes a Promise<Response> — pass the unawaited call
      await expect(configPage.saveAndTest()).toBeOK();
      await expect(configPage).toHaveAlert('success');
    });

    test('should show error alert when health check fails', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      // A URL must be present for the save to succeed and trigger the health check.
      // The mock then intercepts /health and returns a 400 so we can assert the error UI.
      await page.getByLabel('Data source connection URL').fill('http://elasticsearch:9200');
      await configPage.mockHealthCheckResponse({ message: 'Failed to connect to Elasticsearch', status: 'ERROR' }, 400);

      await configPage.saveAndTest();
      await expect(configPage).toHaveAlert('error');
    });

    test('should show error alert when Elasticsearch is unreachable', async ({ createDataSourceConfigPage, page }) => {
      const configPage = await createDataSourceConfigPage({ type: PLUGIN_TYPE });

      // Point at a port nothing is listening on — the backend health check will fail for real
      await page.getByLabel('Data source connection URL').fill('http://localhost:19200');
      await configPage.saveAndTest();
      await expect(configPage).toHaveAlert('error');
    });
  });
});
