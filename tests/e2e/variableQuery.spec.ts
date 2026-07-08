import { expect, test } from '@grafana/plugin-e2e';

// Regression coverage for https://github.com/grafana/grafana/issues/106053.
//
// The bug: for typed-field buckets (boolean/date/ip) Elasticsearch returns a
// numeric `key` alongside a human-readable `key_as_string`. The previous
// `getTerms` implementation returned the raw key as `value` while `text` used
// `key_as_string`, so dashboard variable URLs ended up as `var-X=1` with
// display text "true" — for ad-hoc filters this produced `field|=|1,true` and
// broke substitution.
//
// This spec drives the legacy-variable code path end-to-end (which uses
// `metricFindQuery` → `getTerms`), against a real Elasticsearch index that
// contains a boolean `success` field. The assertion confirms that after the
// variable resolves and after the user picks a different value, the URL uses
// `true`/`false` — not `0`/`1`.
//
// Fixture: tests/e2e/fixtures/auth_events.ndjson
// Datasource: provisioning/datasources/datasources.yml (auth-events-e2e)

const DASHBOARD_TITLE_PREFIX = 'es-bool-variable-';
const HUMAN_READABLE = /^(true|false)$/;
const RAW_NUMERIC = /^[01]$/;

const dashboardWithBoolVariable = (title: string) => ({
  overwrite: true,
  dashboard: {
    title,
    schemaVersion: 39,
    tags: ['e2e', 'bool-variable'],
    time: { from: '2026-03-17T21:00:00Z', to: '2026-03-18T01:00:00Z' },
    panels: [],
    templating: {
      list: [
        {
          // Legacy string-form query — triggers `migrateVariableQuery` ->
          // `legacyQuery` -> `metricFindQuery` -> `getTerms`.
          type: 'query',
          name: 'success',
          label: 'success',
          datasource: { type: 'elasticsearch', uid: 'auth-events-e2e' },
          query: '{"find":"terms","field":"success"}',
          refresh: 1,
          current: { text: '', value: '' },
          hide: 0,
          multi: false,
          includeAll: false,
          sort: 0,
        },
      ],
    },
  },
});

// The fix lives in the externalised plugin's `getTerms`. Some Grafana versions
// (observed: 11.6.x) still load the bundled core elasticsearch datasource even
// with `as_external = true` in grafana.ini — `/api/plugins/elasticsearch/settings`
// reports `module: core:plugin/elasticsearch` rather than
// `public/plugins/elasticsearch/module.js`. On those versions this test would
// correctly detect the upstream bug, but there is no fix to apply from this
// repo. Skip in that scenario rather than fail.
async function externalPluginIsLoaded(page: import('@playwright/test').Page): Promise<boolean> {
  const resp = await page.request.get('/api/plugins/elasticsearch/settings');
  if (!resp.ok()) {
    return false;
  }
  const settings = (await resp.json()) as { module?: string };
  return typeof settings.module === 'string' && settings.module.startsWith('public/plugins/');
}

// Grafana's scenes-based dashboard migration (>= 13.2.0, currently the nightly build)
// changed how dashboard template variables render and sync to the URL, so this test's
// variable no longer resolves in time and var-success stays empty. plugin-e2e's
// grafanaVersion fixture strips the pre-release suffix, so nightly ("13.2.0-<build>")
// reports as "13.2.0". Skip until the E2E tooling supports the new dashboard variables.
// Tracked in https://github.com/grafana/grafana-elasticsearch-datasource/issues/356
const SCENES_DASHBOARD_MIN_VERSION = '13.2.0';
function isGrafanaAtLeast(version: string, target: string): boolean {
  const toParts = (v: string) => v.split('.').map((p) => parseInt(p, 10) || 0);
  const [vMajor, vMinor, vPatch] = toParts(version);
  const [tMajor, tMinor, tPatch] = toParts(target);
  if (vMajor !== tMajor) return vMajor > tMajor;
  if (vMinor !== tMinor) return vMinor > tMinor;
  return vPatch >= tPatch;
}

test.describe('Legacy Query variable on boolean field', () => {
  test.skip(
    ({ grafanaVersion }) => isGrafanaAtLeast(grafanaVersion, SCENES_DASHBOARD_MIN_VERSION),
    'Dashboard variables changed by the scenes migration (Grafana >= 13.2.0). See #356'
  );

  test(
    'aligns value with text so the URL uses "true"/"false", not "1"/"0" (issue #106053)',
    async ({ page }) => {
      test.skip(
        !(await externalPluginIsLoaded(page)),
        'Externalised plugin not loaded; the bug exists in the core in-tree datasource but the fix lives in this repo only'
      );

      const title = `${DASHBOARD_TITLE_PREFIX}${Date.now()}`;
      const createResp = await page.request.post('/api/dashboards/db', {
        data: dashboardWithBoolVariable(title),
      });
      expect(createResp.ok()).toBeTruthy();
      const created = await createResp.json();
      const dashboardUid: string = created.uid;

      try {
        await page.goto(`/d/${dashboardUid}`);

        // Wait for the variable picker to render. The dropdown's data-testid
        // suffix embeds the current value, so use a prefix selector to remain
        // robust whether the picked value is `false` (correct) or `0` (bug).
        const picker = page.locator(
          '[data-testid^="data-testid Dashboard template variables Variable Value DropDown value link"]:not([data-testid$="-input"])'
        );
        await expect(picker).toBeVisible();

        // First assertion: the initial URL set by the auto-selected variable
        // value uses the human-readable form. Before the fix it would be
        // `var-success=0`; after the fix it is `var-success=false`.
        await expect
          .poll(() => new URL(page.url()).searchParams.get('var-success'))
          .toMatch(HUMAN_READABLE);
        expect(new URL(page.url()).searchParams.get('var-success')).not.toMatch(RAW_NUMERIC);

        const initialValue = new URL(page.url()).searchParams.get('var-success')!;
        const otherValue = initialValue === 'true' ? 'false' : 'true';

        // Pick the other value from the dropdown and verify the URL again.
        // This proves the picker also resolves the new value via the same
        // `getTerms` path and writes the human-readable form to the URL.
        await picker.click();
        await page.getByRole('option', { name: otherValue, exact: true }).click();

        await expect
          .poll(() => new URL(page.url()).searchParams.get('var-success'))
          .toBe(otherValue);
        expect(new URL(page.url()).searchParams.get('var-success')).not.toMatch(RAW_NUMERIC);
      } finally {
        await page.request.delete(`/api/dashboards/uid/${dashboardUid}`);
      }
    }
  );
});
