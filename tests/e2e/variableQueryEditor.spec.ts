import { expect, test } from '@grafana/plugin-e2e';
import { type APIRequestContext } from '@playwright/test';

// Fixture data (tests/e2e/fixtures/*.ndjson) covers 2026-03-17T21:25:47Z – 2026-03-18T00:44:47Z UTC.
// Variable preview / query-execution tests that need real data use this window; UI-only
// tests can rely on mocked responses.
const FIXTURE_FROM_ISO = '2026-03-17T21:00:00.000Z';
const FIXTURE_TO_ISO = '2026-03-18T01:00:00.000Z';

const HTTPLOGS_DS_UID = 'httplogs-e2e';

type LegacyVariable = {
  name: string;
  type: 'query';
  datasource: { type: 'elasticsearch'; uid: string };
  // `query` is deliberately `unknown` so tests can stash either a legacy string
  // (e.g. {"find":"terms"...}) or a new object-shaped query and exercise
  // migrateVariableQuery() from the dashboard-load path.
  query: unknown;
  refresh?: number;
};

async function createDashboard(
  request: APIRequestContext,
  args: { title: string; variables: LegacyVariable[]; dashboardRange?: { from: string; to: string } }
): Promise<{ uid: string; url: string }> {
  const body = {
    dashboard: {
      title: args.title,
      tags: ['e2e', 'variable-query'],
      templating: { list: args.variables },
      time: args.dashboardRange
        ? { from: args.dashboardRange.from, to: args.dashboardRange.to }
        : { from: 'now-6h', to: 'now' },
      schemaVersion: 30,
      version: 0,
      panels: [],
    },
    folderUid: '',
    overwrite: true,
  };

  const resp = await request.post('/api/dashboards/db', { data: body });
  if (!resp.ok()) {
    throw new Error(`Failed to create dashboard: ${resp.status()} ${await resp.text()}`);
  }
  const json = await resp.json();
  return { uid: json.uid, url: json.url };
}

async function deleteDashboard(request: APIRequestContext, uid: string): Promise<void> {
  // Best-effort cleanup — don't fail the test if the dashboard was already gone.
  await request.delete(`/api/dashboards/uid/${uid}`).catch(() => undefined);
}

// Navigates to the dashboard's variable edit view. `editIndex` picks which variable
// in the templating list to open. Grafana 11.3.0+ switched this URL from
// `editview=templating` to `editview=variables`; this repo runs 12.x (docker-compose.yaml).
function variableEditUrl(dashboardUrl: string, editIndex: number): string {
  return `${dashboardUrl}?editview=variables&editIndex=${editIndex}`;
}

// `EditorField` in @grafana/plugin-ui renders a <label> without htmlFor, so Playwright's
// getByLabel() doesn't resolve it. The label is a sibling of the <div role="combobox">
// under a shared wrapper, so we anchor on the label text and climb one level.
function comboboxForField(page: import('@playwright/test').Page, label: string) {
  return page.getByText(label, { exact: true }).locator('..').getByRole('combobox');
}

// The "terms → DSL code editor" migration renders Monaco only when the
// elasticsearchRawDSLQuery feature flag is on (gated in QueryEditor/index.tsx).
// plugin-e2e's featureToggles option rewrites window.grafanaBootData.settings.featureToggles
// — it's frontend-only, which is enough here because we only assert on UI rendering.
// Any test that actually *executes* a raw DSL query would need the same toggle enabled
// on the Grafana process (e.g. via GF_FEATURE_TOGGLES_ENABLE), because the plugin's
// backend independently gates query acceptance on the same flag.
// `test.use` is worker-scoped so it must live at file scope; the override is a no-op
// for the other tests in this file.
test.use({ featureToggles: { elasticsearchRawDSLQuery: true } });

// Shapes a mocked /api/ds/query response for the FieldMapping preview query.
// The refId matches `ElasticsearchVariableQueryEditor-VariableQuery` (exported as
// `refId` from ElasticsearchVariableUtils.ts) so the frontend routes the frame
// back into the editor's subscription.
function mockVariablePreviewFrame(fields: Array<{ name: string; values: unknown[] }>) {
  return {
    results: {
      'ElasticsearchVariableQueryEditor-VariableQuery': {
        frames: [
          {
            schema: {
              refId: 'ElasticsearchVariableQueryEditor-VariableQuery',
              fields: fields.map((f) => ({ name: f.name, type: 'string', typeInfo: { frame: 'string' } })),
            },
            data: {
              values: fields.map((f) => f.values),
            },
          },
        ],
      },
    },
  };
}

test.describe('Variable query editor', () => {
  test.describe('new editor rendering', () => {
    test.beforeEach(async ({ variableEditPage }) => {
      await variableEditPage.setVariableType('Query');
      await variableEditPage.datasource.set('elasticsearch');
    });

    test(
      'smoke: renders the full query editor (not a plain text input)',
      { tag: '@plugins' },
      async ({ page }) => {
        // Regression guard against the legacy string-box editor leaking into the
        // new-query path. If migration mis-fires, the Alert banner appears and
        // the query-type radios do not.
        await expect(page.getByRole('alert').filter({ hasText: 'Legacy variable query' })).not.toBeVisible();
        for (const label of ['Metrics', 'Logs', 'Raw Data', 'Raw Document']) {
          await expect(page.getByRole('radio', { name: label })).toBeVisible();
        }
      }
    );

    test('renders Value Field and Text Field combobox rows', async ({ page }) => {
      await expect(page.getByText('Value Field', { exact: true })).toBeVisible();
      await expect(page.getByText('Text Field', { exact: true })).toBeVisible();
    });

    test('does not show the raw-document warning for the default count metric', async ({ page }) => {
      // Default query is { metrics: [{ type: 'count', id: '1' }] }; count is an aggregation,
      // so the "Raw document queries do not return named column fields" hint should be hidden.
      await expect(page.getByText(/Raw document queries do not return named column fields/)).not.toBeVisible();
    });

    test('shows the raw-document warning when switching to Raw Document', async ({ page }) => {
      await page.getByRole('radio', { name: 'Raw Document' }).click();
      await expect(page.getByText(/Raw document queries do not return named column fields/)).toBeVisible();
    });
  });

  test.describe('field mapping', () => {
    test('Value/Text Field dropdowns populate from mocked preview response', async ({
      variableEditPage,
      page,
    }) => {
      await variableEditPage.setVariableType('Query');
      // Mock must be registered before datasource selection because the FieldMapping
      // preview query fires the moment the datasource is picked.
      await variableEditPage.mockQueryDataResponse(
        mockVariablePreviewFrame([
          { name: 'key', values: ['GET', 'POST'] },
          { name: 'Count', values: [12, 34] },
        ])
      );
      await variableEditPage.datasource.set('elasticsearch');

      const valueCombobox = comboboxForField(page, 'Value Field');
      await expect(valueCombobox).toBeVisible();
      await valueCombobox.click();
      // `ElasticsearchVariableQueryEditor-VariableQuery` is the internal refId and must
      // not leak into the options list — convertFieldsToVariableFields filters it out.
      await expect(page.getByRole('option', { name: 'ElasticsearchVariableQueryEditor-VariableQuery' })).not.toBeVisible();
      await expect(page.getByRole('option', { name: 'key', exact: true })).toBeVisible();
      await expect(page.getByRole('option', { name: 'Count', exact: true })).toBeVisible();
    });

    test('allows typing a custom value when no options are returned', async ({ variableEditPage, page }) => {
      await variableEditPage.setVariableType('Query');
      // Empty frame ⇒ no auto-populated options. createCustomValue is enabled on the
      // Combobox so the user can still enter a field name manually.
      await variableEditPage.mockQueryDataResponse(mockVariablePreviewFrame([]));
      await variableEditPage.datasource.set('elasticsearch');

      const valueCombobox = comboboxForField(page, 'Value Field');
      await valueCombobox.click();
      await valueCombobox.fill('host.keyword');
      await expect(page.getByText(/Use custom value/i)).toBeVisible();
    });
  });

  test.describe('legacy query migration (PR #231)', () => {
    // Each test creates a dashboard whose template variable query is stored in the
    // pre-externalisation legacy format, then opens the variable editor. That
    // triggers migrateVariableQuery(), which determines which UI is rendered:
    //
    //   {"find":"terms","field":"X"} → dsl/code ⇒ Monaco editor visible, no legacy banner
    //   {"find":"fields",...}         → legacy_variable ⇒ Alert banner + raw string preserved
    //   plain Lucene string           → legacy_variable ⇒ Alert banner + raw string preserved
    //
    // These map one-to-one with the branches in src/ElasticsearchVariableUtils.ts.

    const createdDashboards: string[] = [];

    test.afterEach(async ({ request }) => {
      // Tear down between tests so retries don't reuse a mutated dashboard.
      while (createdDashboards.length) {
        const uid = createdDashboards.pop()!;
        await deleteDashboard(request, uid);
      }
    });

    test('migrates {"find":"terms","field":"..."} to the DSL code editor', async ({ page, request }) => {
      const { uid, url } = await createDashboard(request, {
        title: `E2E legacy terms ${Date.now()}`,
        variables: [
          {
            name: 'legacy_terms',
            type: 'query',
            datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
            query: '{"find":"terms","field":"method.keyword"}',
            refresh: 1,
          },
        ],
      });
      createdDashboards.push(uid);

      await page.goto(variableEditUrl(url, 0));

      // The legacy-variable Alert must NOT appear; this path routes straight to the
      // new editor in dsl/code mode.
      await expect(page.getByRole('alert').filter({ hasText: 'Legacy variable query' })).not.toBeVisible();

      // dsl/code mode renders the Monaco editor (RawQueryEditor → @grafana/ui CodeEditor
      // → Monaco). The tokenised content isn't stable to assert on, so just assert the
      // Monaco container is present — it's only mounted in code mode.
      await expect(page.locator('.monaco-editor').first()).toBeVisible({ timeout: 15_000 });
    });

    test('shows legacy banner for {"find":"fields",...} and preserves the raw string', async ({
      page,
      request,
    }) => {
      const rawQuery = '{"find":"fields","type":"keyword"}';
      const { uid, url } = await createDashboard(request, {
        title: `E2E legacy fields ${Date.now()}`,
        variables: [
          {
            name: 'legacy_fields',
            type: 'query',
            datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
            query: rawQuery,
            refresh: 1,
          },
        ],
      });
      createdDashboards.push(uid);

      await page.goto(variableEditUrl(url, 0));

      await expect(page.getByRole('alert').filter({ hasText: 'Legacy variable query' })).toBeVisible();
      // The original string must be preserved verbatim in the Query input so the
      // legacy metricFindQuery path still receives the exact same payload.
      await expect(page.locator(`input[value='${rawQuery.replace(/'/g, "\\'")}']`)).toBeVisible();
    });

    test('shows legacy banner for plain Lucene strings', async ({ page, request }) => {
      const rawQuery = 'status:active AND region:eu-*';
      const { uid, url } = await createDashboard(request, {
        title: `E2E legacy lucene ${Date.now()}`,
        variables: [
          {
            name: 'legacy_lucene',
            type: 'query',
            datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
            query: rawQuery,
            refresh: 1,
          },
        ],
      });
      createdDashboards.push(uid);

      await page.goto(variableEditUrl(url, 0));

      await expect(page.getByRole('alert').filter({ hasText: 'Legacy variable query' })).toBeVisible();
      await expect(page.locator(`input[value='${rawQuery.replace(/'/g, "\\'")}']`)).toBeVisible();
    });

    test('leaves a new object-form query untouched (no banner, full editor)', async ({ page, request }) => {
      // Regression guard: a dashboard saved AFTER the new editor shipped stores the
      // query as an object. It must render the new editor, never the legacy banner.
      const { uid, url } = await createDashboard(request, {
        title: `E2E new object-form ${Date.now()}`,
        variables: [
          {
            name: 'new_form',
            type: 'query',
            datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
            query: {
              refId: 'ElasticsearchVariableQueryEditor-VariableQuery',
              query: 'method:GET',
              metrics: [{ type: 'count', id: '1' }],
            },
            refresh: 1,
          },
        ],
      });
      createdDashboards.push(uid);

      await page.goto(variableEditUrl(url, 0));

      await expect(page.getByRole('alert').filter({ hasText: 'Legacy variable query' })).not.toBeVisible();
      await expect(page.getByRole('radio', { name: 'Metrics' })).toBeVisible();
    });
  });

  test.describe('multi-property frame shape (scenes #1236)', () => {
    // The scenes multi-property variable work (grafana-org#555, scenes#1236) relies on
    // query variables emitting data frames with named columns beyond `value`/`text`.
    // ElasticsearchVariableSupport.query → updateFrame preserves those extra columns
    // so a downstream scenes QueryVariable can address them via `${var.columnName}`.
    //
    // This test calls /api/ds/query directly (rather than through the editor UI) because
    // it's asserting on the on-the-wire frame shape, which is what scenes consumes.

    test.describe.configure({ mode: 'serial' });

    async function runVariableQuery(
      request: APIRequestContext,
      target: Record<string, unknown>,
      datasourceUid = HTTPLOGS_DS_UID
    ) {
      return request.post('/api/ds/query', {
        data: {
          from: String(new Date(FIXTURE_FROM_ISO).getTime()),
          to: String(new Date(FIXTURE_TO_ISO).getTime()),
          queries: [
            {
              refId: 'ElasticsearchVariableQueryEditor-VariableQuery',
              datasource: { type: 'elasticsearch', uid: datasourceUid },
              ...target,
            },
          ],
        },
      });
    }

    test('terms aggregation emits a frame with at least two named columns', async ({ request }) => {
      const response = await runVariableQuery(request, {
        queryType: 'lucene',
        query: '',
        metrics: [{ type: 'count', id: '1' }],
        bucketAggs: [
          {
            type: 'terms',
            id: '2',
            field: 'method.keyword',
            settings: { size: '10', order: 'desc', orderBy: '_count' },
          },
        ],
      });
      expect(response.ok()).toBe(true);
      const body = await response.json();
      const frames = body.results?.['ElasticsearchVariableQueryEditor-VariableQuery']?.frames ?? [];
      expect(frames.length).toBeGreaterThan(0);

      const fieldNames = frames.flatMap((f: { schema?: { fields?: Array<{ name: string }> } }) =>
        (f.schema?.fields ?? []).map((field) => field.name)
      );
      // Multi-property support hinges on >1 named column. If this ever regresses to a
      // single value column, scenes' `${var.prop}` addressing breaks for ES variables.
      expect(fieldNames.length).toBeGreaterThanOrEqual(2);
      expect(new Set(fieldNames).size).toBeGreaterThanOrEqual(2);
    });
  });

  test.describe('persistence', () => {
    test('persists value/text field mapping across reloads', async ({ page, request }) => {
      const { uid, url } = await createDashboard(request, {
        title: `E2E persistence ${Date.now()}`,
        variables: [
          {
            name: 'persist_var',
            type: 'query',
            datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
            query: {
              refId: 'ElasticsearchVariableQueryEditor-VariableQuery',
              query: '',
              metrics: [{ type: 'count', id: '1' }],
              meta: {
                valueField: 'method.keyword',
                textField: 'status.keyword',
              },
            },
            refresh: 1,
          },
        ],
      });

      try {
        await page.goto(variableEditUrl(url, 0));
        // Both mapped values must be visible in the comboboxes — even though the options
        // list is empty (createCustomValue=true means the combobox still displays
        // previously-saved values).
        await expect(comboboxForField(page, 'Value Field')).toHaveValue('method.keyword');
        await expect(comboboxForField(page, 'Text Field')).toHaveValue('status.keyword');

        // Hard reload and re-open — confirms the values survive a full page reload,
        // not just in-session state.
        await page.reload();
        await page.goto(variableEditUrl(url, 0));
        await expect(comboboxForField(page, 'Value Field')).toHaveValue('method.keyword');
        await expect(comboboxForField(page, 'Text Field')).toHaveValue('status.keyword');
      } finally {
        await deleteDashboard(request, uid);
      }
    });
  });
});
