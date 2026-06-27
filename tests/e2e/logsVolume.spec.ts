import { expect, test } from '@grafana/plugin-e2e';

// Regression coverage for https://github.com/grafana/grafana/issues/90436.
//
// The bug: when running a Logs query against a datasource with `logLevelField`
// configured, Grafana fires a supplementary log-volume query whose response
// the plugin's backend processes by stripping field labels (see
// field_namer.go::nameFields when keepLabelsInResponse=false) and writing the
// terms bucket value into `frame.name`. Grafana's logs-volume panel reads the
// `level` label off the value field to colour each series, so without
// re-attaching it from `frame.name` every series collapses to "unknown" and
// the volume bar renders grey with a single legend entry.
//
// This spec opens Explore against the `app_logs` index (which has a `level`
// field with values info/warn/error/debug) and asserts the volume legend
// shows per-level entries.
//
// Fixture: tests/e2e/fixtures/app_logs.ndjson
// Datasource: provisioning/datasources/datasources.yml (app-logs-e2e,
// logLevelField=level.keyword to match ES's default `text` + `.keyword`
// multi-field mapping).

const FIXTURE_FROM_ISO = '2026-03-17T21:00:00.000Z';
const FIXTURE_TO_ISO = '2026-03-18T01:00:00.000Z';

// The fix lives in the externalised plugin's `query()` override (it attaches
// `level` labels to supplementary log-volume frames). Some Grafana versions
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

// The raw DSL "Code" editor (and its backend processing) is gated behind the
// `elasticsearchRawDSLQuery` feature toggle. The E2E grafana.ini enables it (see
// docker-compose.yaml), but skip rather than fail if the test runs against an env
// where it is off.
async function rawDslFeatureEnabled(page: import('@playwright/test').Page): Promise<boolean> {
  const resp = await page.request.get('/api/frontend/settings');
  if (!resp.ok()) {
    return false;
  }
  const settings = (await resp.json()) as { featureToggles?: Record<string, boolean> };
  return Boolean(settings.featureToggles?.elasticsearchRawDSLQuery);
}

const exploreUrl = (query: Record<string, unknown>): string => {
  const panes = JSON.stringify({
    explore: {
      datasource: 'app-logs-e2e',
      queries: [{ refId: 'A', datasource: { type: 'elasticsearch', uid: 'app-logs-e2e' }, ...query }],
      range: { from: FIXTURE_FROM_ISO, to: FIXTURE_TO_ISO },
    },
  });
  return `/explore?orgId=1&schemaVersion=1&panes=${encodeURIComponent(panes)}`;
};

// A Logs query built with the visual builder (`metrics: [logs]`, empty Lucene query).
const builderLogsQuery = { metrics: [{ id: '1', type: 'logs' }], query: '' };

// The same Logs query expressed through the raw DSL "Code" editor.
const dslLogsQuery = {
  metrics: [{ id: '1', type: 'logs' }],
  queryType: 'dsl',
  editorType: 'code',
  query: '{"query":{"match_all":{}}}',
};

// Combines the panel's legend entries (rendered as "<level>Total: <n>" in sibling
// elements) into a single string for substring assertions.
async function readVolumeLegendText(page: import('@playwright/test').Page): Promise<string> {
  const legendEntries = page.locator('[class*="legend"], [class*="Legend"]').filter({ hasText: /Total:/ });
  await expect(legendEntries.first()).toBeVisible({ timeout: 15000 });
  return (await legendEntries.allTextContents()).join('|');
}

// Asserts the volume panel rendered the expected per-level breakdown for the
// app_logs fixture (8 info, 3 warn, 3 error, 1 debug). Grafana normalises
// `warn` → `warning`. The legend text packs values together as
// "infoTotal: 8errorTotal: 3..." (no whitespace between entries) so we assert on
// `<level>Total:` substrings rather than word-boundary matches.
//
// Belt-and-braces: a broken volume query collapses everything into a single
// non-zero "unknown" entry. A correct one produces an "unknown" entry only with a
// 0 count (the supplementary query's `missing: "unknown"` placeholder, for which
// there are no documents here). We match non-zero totals directly with `[1-9]\d*`
// rather than a negative lookahead: because entries pack together without
// whitespace (e.g. "unknownTotal: 0infoTotal: 8"), a `(?!0\b)` guard would still
// match the allowed `0` — the `0` is followed by a word char, so `0\b` fails, the
// lookahead passes, and the assertion would wrongly fail on the zero case.
function expectPerLevelVolume(legendText: string): void {
  expect(legendText).toMatch(/infoTotal:/);
  expect(legendText).toMatch(/warningTotal:/);
  expect(legendText).toMatch(/errorTotal:/);
  expect(legendText).toMatch(/debugTotal:/);
  expect(legendText).toMatch(/unknownTotal:\s*0/);
  expect(legendText).not.toMatch(/unknownTotal:\s*[1-9]\d*/);
}

test.describe('Logs volume on a datasource with logLevelField', () => {
  test(
    'renders per-level legend entries, not a single "unknown" bucket (issue #90436)',
    { tag: '@plugins' },
    async ({ page }) => {
      test.skip(
        !(await externalPluginIsLoaded(page)),
        'Externalised plugin not loaded; the bug exists in the core in-tree datasource but the fix lives in this repo only'
      );

      await page.goto(exploreUrl(builderLogsQuery));

      // The supplementary log-volume request needs time to complete and render its
      // legend. The bug collapses everything into a single "unknown" entry; the fix
      // produces one entry per level.
      const legendText = await readVolumeLegendText(page);
      expectPerLevelVolume(legendText);
    }
  );

  // Regression coverage for https://github.com/grafana/grafana-elasticsearch-datasource/issues/112.
  //
  // In the raw DSL "Code" editor with the Logs query type, the logs render but the
  // logs-volume panel failed with "Failed to load log volume for this query — unable
  // to parse response from datasource". The supplementary log-volume query is
  // synthesised builder-style (count metric + date_histogram) but inherits
  // queryType:"dsl"; the backend re-parsed aggregations from the raw DSL body (which
  // has none), discarded the synthesised date_histogram, and returned an
  // aggregation-less response that core's volume provider couldn't parse.
  //
  // This drives the exact scenario through the Explore URL (no Monaco interaction) and
  // asserts the volume renders the same per-level breakdown as the builder path, with
  // no failure alert. The fix lives in the backend, so it only applies to the
  // externalised plugin (and needs the elasticsearchRawDSLQuery toggle).
  test('renders logs volume for a raw DSL (Code editor) logs query (issue #112)', { tag: '@plugins' }, async ({
    page,
  }) => {
    test.skip(
      !(await externalPluginIsLoaded(page)),
      'Externalised plugin not loaded; the raw-DSL logs-volume fix lives in this repo only'
    );
    test.skip(
      !(await rawDslFeatureEnabled(page)),
      'elasticsearchRawDSLQuery feature toggle is disabled; the raw DSL editor is unavailable'
    );

    await page.goto(exploreUrl(dslLogsQuery));

    // Wait for the volume to resolve and assert the per-level breakdown. When the bug
    // is present the volume never renders (the panel shows the failure alert instead),
    // so this wait fails — which is the correct regression signal.
    const legendText = await readVolumeLegendText(page);
    expectPerLevelVolume(legendText);

    // Belt-and-braces: the reported symptom is this alert in place of a histogram.
    await expect(page.getByText('Failed to load log volume')).toHaveCount(0);
  });
});
