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

const exploreUrl = (): string => {
  const panes = JSON.stringify({
    explore: {
      datasource: 'app-logs-e2e',
      queries: [
        {
          refId: 'A',
          datasource: { type: 'elasticsearch', uid: 'app-logs-e2e' },
          metrics: [{ id: '1', type: 'logs' }],
          query: '',
        },
      ],
      range: { from: FIXTURE_FROM_ISO, to: FIXTURE_TO_ISO },
    },
  });
  return `/explore?orgId=1&schemaVersion=1&panes=${encodeURIComponent(panes)}`;
};

test.describe('Logs volume on a datasource with logLevelField', () => {
  test(
    'renders per-level legend entries, not a single "unknown" bucket (issue #90436)',
    { tag: '@plugins' },
    async ({ page }) => {
      await page.goto(exploreUrl());

      // Give the supplementary log-volume request time to complete and the
      // panel to render its legend. The legend lists "<level>Total: <n>"
      // rows in separate sibling elements — combine them all into a single
      // string for the substring assertions below. The bug collapses
      // everything into a single "unknown" entry; the fix produces one
      // entry per level.
      const legendEntries = page.locator('[class*="legend"], [class*="Legend"]').filter({
        hasText: /Total:/,
      });

      await expect(legendEntries.first()).toBeVisible({ timeout: 15000 });

      const allEntries = await legendEntries.allTextContents();
      const legendText = allEntries.join('|');

      // The fixture has 8 info, 3 warn, 3 error, 1 debug. With the fix,
      // Grafana also normalises `warn` → `warning`. The legend text packs
      // values together as "infoTotal: 8errorTotal: 3..." (no whitespace
      // between entries) so we assert on `<level>Total:` substrings rather
      // than word-boundary matches. Without the fix the only entry would
      // be a non-zero `unknownTotal`.
      expect(legendText).toMatch(/infoTotal:/);
      expect(legendText).toMatch(/warningTotal:/);
      expect(legendText).toMatch(/errorTotal:/);
      expect(legendText).toMatch(/debugTotal:/);

      // Belt-and-braces: the bug collapses everything to "unknown" with a
      // non-zero total. The fix produces an "unknown" entry only with a 0
      // count (the supplementary query's `missing: "unknown"` placeholder
      // for documents lacking the level field, of which there are none).
      // Match the explicit "unknownTotal: 0" rendering and reject any
      // "unknown" line with a non-zero total.
      expect(legendText).toMatch(/unknownTotal:\s*0/);
      expect(legendText).not.toMatch(/unknownTotal:\s*(?!0\b)\d+/);
    }
  );
});
