import { expect, test } from '@grafana/plugin-e2e';
import { type Locator, type Page, type Response } from '@playwright/test';

import { QueryType } from '../src/types';
import { env, isQueryRequestUrl } from './testEnv';

// QUERY_TYPE_LABELS from src/configuration/utils.ts cannot be imported here
// because its import chain pulls in @grafana/ui which requires browser globals (window).
// The labels are defined inline below and should be kept in sync with that source constant.
const QUERY_TYPE_LABELS: Array<{ value: QueryType; label: string }> = [
  { value: 'metrics', label: 'Metrics' },
  { value: 'logs', label: 'Logs' },
  { value: 'raw_data', label: 'Raw Data' },
  { value: 'raw_document', label: 'Raw Document' },
];

// Locally this is the fixture window (seed 42, 2026-03-17T21:25:47Z – 2026-03-18T00:44:47Z UTC);
// on Cloud it is the last few hours of continuously generated data. Either way it is kept tight
// so the Logs histogram query stays under Elasticsearch's 65536-bucket limit, and both ends are
// ISO strings so the Grafana Explore pane URL is timezone-unambiguous.
const FIXTURE_FROM_ISO = env.from;
const FIXTURE_TO_ISO = env.to;

// Grafana 13 migrated query editor row selectors from aria-label to data-testid
// (grafana/grafana#121784). This helper matches both so tests work across versions
// until @grafana/plugin-e2e ships a fix and this repo upgrades.
function getQueryEditorRow(page: Page, refId: string): Locator {
  return page
    .locator('[data-testid="data-testid Query editor row"], [aria-label="Query editor row"]')
    .filter({ has: page.locator(`[data-testid="data-testid Query editor row title ${refId}"], [aria-label="Query editor row title ${refId}"]`) });
}

// The plugin's default Metrics query. Pinning it in the Explore URL gives every test the same
// starting state.
const DEFAULT_QUERY = {
  query: '',
  metrics: [{ id: '1', type: 'count' }],
  bucketAggs: [{ id: '2', type: 'date_histogram', field: '@timestamp', settings: { interval: 'auto' } }],
  timeField: '@timestamp',
  editorType: 'builder',
};

test.describe('Query editor', () => {
  // Explore restores the account's last query, and on the shared Cloud instance that state
  // outlives the run that wrote it. A mode radio can therefore already be selected when a test
  // starts, so clicking it is a no-op and the editor never performs the transition being
  // asserted on. Navigating with an explicit query pins the starting state instead, and skips
  // the datasource picker — which reports no error when it fails to select.
  //
  // explorePage is requested so its own goto() runs first; the goto below then pins the pane.
  test.beforeEach(async ({ explorePage, page }) => {
    await page.goto(exploreUrl(env.defaultDatasourceUid, { queryOverrides: DEFAULT_QUERY }));
    await expect(getQueryEditorRow(page, 'A').getByRole('radio', { name: 'Metrics' })).toBeChecked();
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
      // On Grafana <12 the core elasticsearch datasource still ships a Slate-based
      // contentEditable; on 12+ the externalised plugin renders a plain <input>.
      // Match both: getByRole('textbox') covers each, and the assertion reads value || textContent.
      const queryField = queryRow.getByRole('textbox').first();
      await expect(queryField).toBeVisible();
      await queryField.click();
      await page.keyboard.type('status:200');
      await expect
        .poll(() => queryField.evaluate((el) => (el as HTMLInputElement).value || el.textContent || ''))
        .toContain('status:200');
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
      const responsePromise = page.waitForResponse((resp) => isQueryRequestUrl(resp.url()));
      await queryRow.getByRole('radio', { name: 'Metrics' }).click();

      const response = await responsePromise;
      await expect(response).toBeOK();
    });

    test('executes a Logs query and receives a response', async ({ explorePage, page }) => {
      const queryRow = getQueryEditorRow(page, 'A');
      // Set up mock and waitForResponse BEFORE the mode switch so the auto-query is caught.
      await explorePage.mockQueryDataResponse({ results: { A: { frames: [] } } });
      const responsePromise = page.waitForResponse((resp) => isQueryRequestUrl(resp.url()));
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
  options?: {
    luceneQuery?: string;
    metricsType?: 'logs' | 'raw_data';
    bucketAggs?: Array<Record<string, unknown>>;
    queryOverrides?: Record<string, unknown>;
  }
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
  if (options?.bucketAggs) {
    query.metrics = [{ id: '1', type: 'count' }];
    query.bucketAggs = options.bucketAggs;
  }
  if (options?.queryOverrides) {
    Object.assign(query, options.queryOverrides);
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
    if (!isQueryRequestUrl(r.url()) || !r.ok()) {
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

  test.describe('logs index', () => {
    test('Metrics mode: count query returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.logsUid));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Metrics mode: Lucene filter returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.logsUid, { luceneQuery: env.logs.luceneFilter }));
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
      await page.goto(exploreUrl(env.logsUid, { metricsType: 'logs' }));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Metrics mode: high-cardinality terms with auto interval does not exceed max buckets', async ({ page }) => {
      // One distinct term per document multiplied by the ~1,000+ time buckets a panel-width auto
      // interval produces would need well over the Elasticsearch search.max_buckets
      // default (65,535) and fail with "Trying to create too many buckets". The backend
      // widens the auto interval to fit instead (#383).
      // Resolve on any results.A (error or frames) so a regression fails the assertion
      // below instead of timing out in waitForMainQueryResponse().
      let body: any;
      const responsePromise = page.waitForResponse(async (r: Response) => {
        if (!isQueryRequestUrl(r.url())) {
          return false;
        }
        const b = await r.json().catch(() => null);
        if (!b?.results?.A) {
          return false;
        }
        body = b;
        return true;
      });
      await page.goto(
        exploreUrl(env.logsUid, {
          bucketAggs: [
            // order/orderBy match the query builder's defaults. Without them the
            // backend serialises an empty terms "order" object which Elasticsearch
            // rejects (a separate pre-existing bug, not what this test covers).
            {
              id: '2',
              type: 'terms',
              field: env.logs.highCardinalityField,
              settings: { size: '0', order: 'desc', orderBy: '_term' },
            },
            { id: '3', type: 'date_histogram', field: '@timestamp', settings: { interval: 'auto' } },
          ],
        })
      );
      await responsePromise;
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Raw Data mode: returns documents with the expected field', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.logsUid, { metricsType: 'raw_data' }));
      const { body } = await responsePromise;
      const frames: Array<{ schema?: { fields?: Array<{ name: string }> } }> = body.results?.A?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);
      const fieldNames = frames.flatMap((f) => (f.schema?.fields ?? []).map((field) => field.name));
      expect(fieldNames).toContain(env.logs.rawDataField);
    });
  });

  test.describe('metrics index', () => {
    test('Metrics mode: count query returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.metricsUid));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Metrics mode: Lucene filter on a tag field returns results', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.metricsUid, { luceneQuery: env.metrics.luceneFilter }));
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });

    test('Raw Data mode: returns documents with the expected field', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(exploreUrl(env.metricsUid, { metricsType: 'raw_data' }));
      const { body } = await responsePromise;
      const frames: Array<{ schema?: { fields?: Array<{ name: string }> } }> = body.results?.A?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);
      const fieldNames = frames.flatMap((f) => (f.schema?.fields ?? []).map((field) => field.name));
      expect(fieldNames).toContain(env.metrics.rawDataField);
    });

    test('Metrics mode: sum bucket composite aggregation returns summed per-host maxima', async ({ page }) => {
      const responsePromise = waitForMainQueryResponse(page);
      await page.goto(
        exploreUrl(env.metricsUid, {
          queryOverrides: {
            metrics: [
              {
                id: '1',
                type: 'sum_bucket',
                field: env.metrics.numericField,
                // host is dynamically mapped as text; terms aggregations need the keyword subfield.
                settings: { metric: 'max', groupBy: env.metrics.groupByField, limit: '500' },
              },
            ],
            bucketAggs: [{ id: '2', type: 'date_histogram', field: '@timestamp', settings: { interval: 'auto' } }],
          },
        })
      );
      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      const frames = body.results?.A?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);
      const values: Array<number | null> = frames[0]?.data?.values?.[1] ?? [];
      expect(values.some((v) => v !== null && v > 0)).toBe(true);
    });

    test('Metrics mode: sum bucket settings UI drives group by and returns results', async ({ page }) => {
      // Pinned through the URL rather than the datasource picker and time-range control: the
      // picker reports no error when a selection does not take, which turns a mis-set datasource
      // into a timeout somewhere further down the test. The UI interactions below — which are
      // what this test actually covers — are unaffected.
      await page.goto(exploreUrl(env.metricsUid, { queryOverrides: DEFAULT_QUERY }));

      const queryRow = getQueryEditorRow(page, 'A');
      await expect(queryRow.getByRole('radio', { name: 'Metrics' })).toBeChecked();

      // Switch the metric type from the default Count to Sum Bucket via the metric segment.
      await queryRow.getByRole('button', { name: 'Count' }).click();
      await page.getByRole('option', { name: 'Sum Bucket', exact: true }).click();

      // Set the field the inner per-group metric is calculated on.
      await queryRow.getByRole('button', { name: 'Select Field' }).click();
      await page.getByRole('option', { name: env.metrics.numericField, exact: true }).click();

      // Open the collapsed settings section, whose visible label is the row description.
      await queryRow.getByRole('button', { name: /Group by: not set/i }).click();

      // Set up the response wait before the action that fires the query we care about,
      // so it can't resolve on an earlier (still-invalid, missing groupBy) request.
      const responsePromise = waitForMainQueryResponse(page);

      // The Group By segment is the only remaining "Select Field" placeholder once the
      // metric field above has a value.
      await queryRow.getByRole('button', { name: 'Select Field' }).click();
      await page.getByRole('option', { name: env.metrics.groupByField, exact: true }).click();

      const { response, body } = await responsePromise;
      expect(response.ok()).toBe(true);
      expect(body.results?.A?.error).toBeUndefined();
      expect(body.results?.A?.frames?.length).toBeGreaterThan(0);
    });
  });
});
