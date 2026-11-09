import { expect, test } from '@playwright/test';

import { transportErrorFor } from './testEnv';

// Pins the transport/rejection split cloud.setup.ts and macros.spec.ts rely on. Loosening it turns
// rejections into retries; tightening it brings back the PDC cold-dial flake. No Grafana needed.

function fakeResponse(status: number, body: unknown) {
  return {
    status: () => status,
    json: async () => (typeof body === 'string' ? JSON.parse(body) : body),
  };
}

const errorFor = (error: string) => ({ results: { A: { error } } });

const cases: Array<{ name: string; status: number; body: unknown; transport: boolean }> = [
  {
    name: 'PDC socks dial failure',
    status: 400,
    body: errorFor(
      'Post "https://es.example/_query": socks connect tcp private-datasource-connect.example:443->es.example:9200: unknown error host unreachable'
    ),
    transport: true,
  },
  {
    name: 'Go client EOF',
    status: 400,
    body: errorFor('Post "https://es.example/_msearch": EOF'),
    transport: true,
  },
  {
    name: 'max_buckets rejection',
    status: 400,
    body: errorFor('Trying to create too many buckets. Must be less than or equal to: [65535] but was [65536].'),
    transport: false,
  },
  {
    name: 'unknown index rejection',
    status: 400,
    body: errorFor('line 1:6: Unknown index [$__indexes]'),
    transport: false,
  },
  {
    name: "ES|QL parse error mentioning '<EOF>'",
    status: 400,
    body: errorFor("line 1:12: mismatched input '<EOF>' expecting {'dissect', 'eval', 'limit'}"),
    transport: false,
  },
  { name: 'successful answer', status: 200, body: { results: { A: { frames: [] } } }, transport: false },
  { name: 'non-JSON edge page', status: 502, body: '<html>Bad Gateway</html>', transport: true },
  {
    name: 'Grafana {"message"} error without results',
    status: 500,
    body: { message: 'Internal error' },
    transport: true,
  },
];

test.describe('transportErrorFor', () => {
  for (const c of cases) {
    test(`${c.name} → ${c.transport ? 'transport' : 'reply'}`, async () => {
      const result = await transportErrorFor(fakeResponse(c.status, c.body));
      if (c.transport) {
        expect(result).not.toBeNull();
      } else {
        expect(result).toBeNull();
      }
    });
  }
});
