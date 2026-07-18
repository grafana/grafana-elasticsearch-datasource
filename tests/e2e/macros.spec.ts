import { expect, test } from '@grafana/plugin-e2e';
import { type APIRequestContext } from '@playwright/test';

// Fixture data (tests/e2e/fixtures/*.ndjson) covers 2026-03-17T21:25:47Z – 2026-03-18T00:44:47Z UTC.
const FIXTURE_FROM_ISO = '2026-03-17T21:00:00.000Z';
const FIXTURE_TO_ISO = '2026-03-18T01:00:00.000Z';

const HTTPLOGS_DS_UID = 'httplogs-e2e';
const HTTPLOGS_DOC_COUNT = 200;

const INTERVAL_MS = 20000;

interface Frame {
  schema?: { name?: string; fields?: Array<{ name: string }> };
  data?: { values: unknown[][] };
}

// Backend macro handling ($__interval, $__interval_ms, $__index and the internal
// $__interval_msms auto-interval placeholder) is only observable on the wire, so these
// tests call /api/ds/query directly and assert on the returned frames — the same
// approach as the frame-shape tests in variableQueryEditor.spec.ts. Macro expansion
// happens in the plugin backend after this request, so a literal macro in the payload
// reaching Elasticsearch unexpanded fails loudly (ES rejects it as an invalid interval
// or unknown index).
async function runQuery(request: APIRequestContext, target: Record<string, unknown>) {
  return request.post('/api/ds/query', {
    data: {
      from: String(new Date(FIXTURE_FROM_ISO).getTime()),
      to: String(new Date(FIXTURE_TO_ISO).getTime()),
      queries: [
        {
          refId: 'A',
          datasource: { type: 'elasticsearch', uid: HTTPLOGS_DS_UID },
          intervalMs: INTERVAL_MS,
          maxDataPoints: 780,
          ...target,
        },
      ],
    },
  });
}

async function framesForA(response: { json(): Promise<unknown> }): Promise<Frame[]> {
  const body = (await response.json()) as { results?: Record<string, { frames?: Frame[] }> };
  return body.results?.['A']?.frames ?? [];
}

test.describe('backend macro handling', () => {
  test('metrics query with auto date histogram interval expands $__interval_msms', async ({ request }) => {
    // interval:auto makes the backend emit the internal $__interval_msms placeholder as
    // fixed_interval; if it survives to Elasticsearch unexpanded, ES rejects the request.
    const response = await runQuery(request, {
      queryType: 'lucene',
      query: '*',
      metrics: [{ type: 'count', id: '1' }],
      bucketAggs: [{ type: 'date_histogram', id: '2', field: '@timestamp', settings: { interval: 'auto' } }],
      timeField: '@timestamp',
    });
    expect(response.ok()).toBe(true);

    const frames = await framesForA(response);
    expect(frames.length).toBeGreaterThan(0);

    // Uniform bucket spacing equal to the requested interval proves the expanded
    // value ("20000ms") reached Elasticsearch.
    const timestamps = (frames[0].data?.values[0] ?? []) as number[];
    expect(timestamps.length).toBeGreaterThan(2);
    for (let i = 1; i < timestamps.length; i++) {
      expect(timestamps[i] - timestamps[i - 1]).toBe(INTERVAL_MS);
    }

    const counts = (frames[0].data?.values[1] ?? []) as Array<number | null>;
    const total = counts.reduce<number>((sum, v) => sum + (v ?? 0), 0);
    expect(total).toBe(HTTPLOGS_DOC_COUNT);
  });

  test('raw DSL query expands $__interval and $__interval_ms', async ({ request }) => {
    const from = new Date(FIXTURE_FROM_ISO).getTime();
    const to = new Date(FIXTURE_TO_ISO).getTime();
    // The avg metric's painless script is the expanded $__interval_ms itself, so every
    // bucket's value equals the interval in milliseconds — the expansion is read back
    // directly from the data.
    const dsl = {
      size: 0,
      query: { bool: { filter: [{ range: { '@timestamp': { gte: from, lte: to, format: 'epoch_millis' } } }] } },
      aggs: {
        '2': {
          date_histogram: {
            field: '@timestamp',
            fixed_interval: '$__interval',
            min_doc_count: 0,
            extended_bounds: { min: from, max: to },
            format: 'epoch_millis',
          },
          aggs: { '1': { avg: { script: { source: '$__interval_ms' } } } },
        },
      },
    };

    // The metrics/bucketAggs model mirrors what the query editor sends alongside the DSL
    // string; the response parser needs it to shape the aggregation response into frames.
    const response = await runQuery(request, {
      queryType: 'dsl',
      query: JSON.stringify(dsl),
      metrics: [{ type: 'avg', id: '1' }],
      bucketAggs: [{ type: 'date_histogram', id: '2', field: '@timestamp', settings: { interval: 'auto' } }],
      timeField: '@timestamp',
    });
    expect(response.ok()).toBe(true);

    const frames = await framesForA(response);
    expect(frames.length).toBeGreaterThan(0);

    const timestamps = (frames[0].data?.values[0] ?? []) as number[];
    expect(timestamps.length).toBeGreaterThan(2);
    for (let i = 1; i < timestamps.length; i++) {
      expect(timestamps[i] - timestamps[i - 1]).toBe(INTERVAL_MS);
    }

    const values = ((frames[0].data?.values[1] ?? []) as Array<number | null>).filter((v) => v !== null);
    expect(values.length).toBeGreaterThan(0);
    for (const v of values) {
      expect(v).toBe(INTERVAL_MS);
    }
  });

  test('ES|QL query expands $__index to the configured index', async ({ request }) => {
    const response = await runQuery(request, {
      queryType: 'esql',
      query: 'FROM $__index | STATS c = COUNT(*) BY method | SORT method',
    });
    expect(response.ok()).toBe(true);

    const frames = await framesForA(response);
    expect(frames.length).toBeGreaterThan(0);

    const fieldNames = (frames[0].schema?.fields ?? []).map((f) => f.name);
    expect(fieldNames).toContain('c');
    expect(fieldNames).toContain('method');

    const counts = (frames[0].data?.values[fieldNames.indexOf('c')] ?? []) as number[];
    const total = counts.reduce((sum, v) => sum + v, 0);
    expect(total).toBe(HTTPLOGS_DOC_COUNT);
  });

  test('ES|QL query expands $__index at every occurrence', async ({ request }) => {
    const response = await runQuery(request, {
      queryType: 'esql',
      query: 'FROM $__index,$__index | STATS c = COUNT(*)',
    });
    expect(response.ok()).toBe(true);

    const frames = await framesForA(response);
    expect(frames.length).toBeGreaterThan(0);
    const counts = (frames[0].data?.values[0] ?? []) as number[];
    // Repeating the same index in FROM does not double-count documents.
    expect(counts[0]).toBe(HTTPLOGS_DOC_COUNT);
  });

  test('tokens that merely start with a macro name are not expanded', async ({ request }) => {
    // Guards the parser semantics: $__indexes must NOT be treated as $__index + "es"
    // (the pre-macropro substring replacement would have produced "httplogses").
    const response = await runQuery(request, {
      queryType: 'esql',
      query: 'FROM $__indexes | LIMIT 1',
    });
    expect(response.ok()).toBe(false);

    const body = (await response.json()) as { results?: Record<string, { error?: string }> };
    const error = body.results?.['A']?.error ?? '';
    expect(error).toContain('$__indexes');
    expect(error).not.toContain('httplogses');
  });
});
