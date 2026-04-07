import { expect, test } from '@grafana/plugin-e2e';
import { type Locator, type Page, type Response } from '@playwright/test';

import { QueryType } from '../src/types';

// QUERY_TYPE_LABELS from src/configuration/utils.ts cannot be imported here
// because its import chain pulls in @grafana/ui which requires browser globals (window).
// The labels are defined inline below and should be kept in sync with that source constant.
const QUERY_TYPE_LABELS: Array<{ value: QueryType; label: string }> = [
  { value: 'metrics', label: 'Metrics' },
  { value: 'logs', label: 'Logs' },
  { value: 'raw_data', label: 'Raw Data' },
  { value: 'raw_document', label: 'Raw Document' },
];

// Fixture data (tests/e2e/fixtures/*.ndjson) was generated with seed 42 for reproducibility.
// It covers 2026-03-17T21:25:47Z – 2026-03-18T00:44:47Z UTC.
// The window is kept tight (~4h) to stay under Elasticsearch's 65536-bucket limit,
// which the Logs histogram query hits on wider ranges.
// ISO strings are used so the Grafana Explore pane URL is timezone-unambiguous.
const FIXTURE_FROM_ISO = '2026-03-17T21:00:00.000Z';
const FIXTURE_TO_ISO = '2026-03-18T01:00:00.000Z';

// Grafana 13 migrated query editor row selectors from aria-label to data-testid
// (grafana/grafana#121784). This helper matches both so tests work across versions
// until @grafana/plugin-e2e ships a fix and this repo upgrades.
function getQueryEditorRow(page: Page, refId: string): Locator {
  return page
    .locator('[data-testid="data-testid Query editor row"], [aria-label="Query editor row"]')
    .filter({ has: page.locator(`[data-testid="data-testid Query editor row title ${refId}"], [aria-label="Query editor row title ${refId}"]`) });
}

// All tests select the datasource explicitly since other test suites may change state.
// The elasticsearch datasource is provisioned as the default, so it is always available.
test.describe('Query editor', () => {
  test.beforeEach(async ({ explorePage }) => {
    // explorePage.goto() is called by the fixture before this hook runs.
    // elasticsearch is the default — datasource.set() confirms the selection without
    // triggering a new query (Grafana treats it as a no-op when unchanged).
    await explorePage.datasource.set('elasticsearch');
  });

  test.describe('rendering', () => {
    test(
      'smoke: renders all query type options',
      { tag: '@plugins' },
      async ({ page }) => {
        const queryRow = getQueryEditorRow(page, 'A');

        for (const { label } of QUERY_TYPE_LABELS) {
          await expect(queryRow.getByRole('radio', { name: label })).toBeVisible();
        }
      }
    );

    test('renders Lucene query field in all modes', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');

      for (const { label } of QUERY_TYPE_LABELS) {
        await queryRow.getByRole('radio', { name: label }).click();
        await expect(queryRow.getByText('Lucene Query', { exact: true })).toBeVisible();
      }
    });
  });

  test.describe('Metrics mode', () => {
    test.beforeEach(async ({ page }) => {
      await getQueryEditorRow(page, 'A').getByRole('radio', { name: 'Metrics' }).click();
    });

    test('shows Metric row with default Count metric', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Metric (1)')).toBeVisible();
      await expect(queryRow.getByRole('button', { name: 'Count' })).toBeVisible();
    });

    test('shows Group By row with default Date Histogram on @timestamp', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Group By')).toBeVisible();
      await expect(queryRow.getByRole('button', { name: 'Date Histogram' })).toBeVisible();
      await expect(queryRow.getByRole('button', { name: '@timestamp' })).toBeVisible();
    });

    test('shows Alias field', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByRole('textbox', { name: 'Alias' })).toBeVisible();
    });

    test('can enter a Lucene query string', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // The Lucene query input is a CodeMirror contenteditable — the first textbox in the row
      const queryField = queryRow.getByRole('textbox').first();
      await expect(queryField).toBeVisible();
      await queryField.click();
      await page.keyboard.type('status:200');
      await expect(queryField).toContainText('status:200');
    });
  });

  test.describe('Logs mode', () => {
    test.beforeEach(async ({ page }) => {
      await getQueryEditorRow(page, 'A').getByRole('radio', { name: 'Logs' }).click();
    });

    test('hides the Alias field', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByRole('textbox', { name: 'Alias' })).not.toBeVisible();
    });

    test('hides the Group By row', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Group By')).not.toBeVisible();
    });

    test('hides the Metric row', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Metric (1)')).not.toBeVisible();
    });
  });

  test.describe('Raw Data mode', () => {
    test.beforeEach(async ({ page }) => {
      await getQueryEditorRow(page, 'A').getByRole('radio', { name: 'Raw Data' }).click();
    });

    test('hides the Alias field', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByRole('textbox', { name: 'Alias' })).not.toBeVisible();
    });

    test('hides the Group By row', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Group By')).not.toBeVisible();
    });

    test('hides the Metric row', async ({ page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByText('Metric (1)')).not.toBeVisible();
    });
  });

  test.describe('query execution', () => {
    test('executes a Metrics query and receives a response', async ({ explorePage, page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // Metrics is the default mode — clicking it again won't trigger an auto-query.
      // Switch to Logs first so that switching back to Metrics fires an auto-query request.
      await queryRow.getByRole('radio', { name: 'Logs' }).click();

      // Set up mock and waitForResponse before the mode switch that triggers the query
      await explorePage.mockQueryDataResponse({ results: { A: { frames: [] } } });
      const responsePromise = page.waitForResponse((resp) => resp.url().includes('/api/ds/query'));
      await queryRow.getByRole('radio', { name: 'Metrics' }).click();

      const response = await responsePromise;
      await expect(response).toBeOK();
    });

    test('executes a Logs query and receives a response', async ({ explorePage, page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // Set up mock and waitForResponse BEFORE the mode switch so the auto-query is caught.
      await explorePage.mockQueryDataResponse({ results: { A: { frames: [] } } });
      const responsePromise = page.waitForResponse((resp) => resp.url().includes('/api/ds/query'));
      await queryRow.getByRole('radio', { name: 'Logs' }).click();
      const response = await responsePromise;
      await expect(response).toBeOK();
    });
  });
});

// These tests use real fixture data loaded into the httplogs and infra Elasticsearch indices.
//
// Each test navigates directly to an Explore URL with the target query mode pre-encoded in
// the panes parameter. This fires exactly one query per test with no mode-switch races:
//
//   - Metrics/Raw Data: one query fires on navigation; no supplementary queries in these modes.
//   - Logs: fires a main query AND a supplementary log-volume histogram query. The log-volume
//     response has no results.A key (it uses a different refId), so waitForQueryDataResponse()
//     is replaced with a page.waitForResponse() predicate that only resolves when results.A
//     is present.
//
// response.json() must be called immediately after receiving the response; calling
// it after other awaits causes Playwright to lose the CDP body reference.
function exploreUrl(
  datasourceUid: string,
  options?: { luceneQuery?: string; metricsType?: 'logs' | 'raw_data' }
): string {
  const query: Record<string, unknown> = {
    refId: 'A',
    datasource: { type: 'elasticsearch', uid: datasourceUid },
  };
  if (options?.luceneQuery) {
    query.query = options.luceneQuery;
  }
  if (options?.metricsType) {
    query.metrics = [{ id: '1', type: options.metricsType }];
  }
  const panes = JSON.stringify({
    explore: {
      datasource: datasourceUid,
      queries: [query],
      range: { from: FIXTURE_FROM_ISO, to: FIXTURE_TO_ISO },
    },
  });
  return `/explore?orgId=1&schemaVersion=1&panes=${encodeURIComponent(panes)}`;
}

// Waits for the first /api/ds/query response where results.A.frames is an array.
// Skips supplementary log-volume responses which use a different refId (no results.A).
async function waitForMainQueryResponse(page: Page): Promise<{ response: Response; body: any }> {
  let body: any;
  const response = await page.waitForResponse(async (r: Response) => {
    if (!r.url().includes('/api/ds/query') || !r.ok()) {
      return false;
    }
    const b = await r.json().catch(() => null);
    if (!Array.isArray(b?.results?.A?.frames)) {
      return false;
    }
    body = b;
    return true;
  });
  return { response, body };
}

test.describe('Query editor with fixture data', () => {
  // Serialize fixture-data tests so they don't compete for the shared ES instance.
  test.describe.configure({ mode: 'serial' });

  test.describe('httplogs index', () => {
    test('Metrics mode: count query returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('httplogs-e2e'));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Metrics mode: Lucene filter on method:GET returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('httplogs-e2e', { luceneQuery: 'method:GET' }));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Logs mode: returns log entries', async ({ page }) => {
      // Logs mode fires a supplementary log-volume query alongside the main query.
      // waitForMainQueryResponse() skips the supplementary (no results.A) and resolves
      // only when the main log-entries response arrives.
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('httplogs-e2e', { metricsType: 'logs' }));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Raw Data mode: returns documents with statusCode field', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('httplogs-e2e', { metricsType: 'raw_data' }));
      const { body } = await responsePromise;
      const frames: Array<{ schema?: { fields?: Array<{ name: string }> } }> = body.results?.A?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);
      const fieldNames = frames.flatMap((f) => (f.schema?.fields ?? []).map((field) => field.name));
      expect(fieldNames).toContain('statusCode');
    });
  });

  test.describe('infra index', () => {
    test('Metrics mode: count query returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('infra-e2e'));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Metrics mode: Lucene filter on role:web returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('infra-e2e', { luceneQuery: 'role:web' }));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Raw Data mode: returns documents with host field', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl('infra-e2e', { metricsType: 'raw_data' }));
      const { body } = await responsePromise;
      const frames: Array<{ schema?: { fields?: Array<{ name: string }> } }> = body.results?.A?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);
      const fieldNames = frames.flatMap((f) => (f.schema?.fields ?? []).map((field) => field.name));
      expect(fieldNames).toContain('host');
    });
  });
});
