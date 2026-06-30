import { ElasticsearchDataQuery } from '../../dataquery.gen';

import { esqlValidator } from './esql';

const esqlQuery = (query: string, overrides: Partial<ElasticsearchDataQuery> = {}): ElasticsearchDataQuery => ({
  refId: 'A',
  queryType: 'esql',
  query,
  ...overrides,
});

describe('esqlValidator', () => {
  it('returns null for a valid ES|QL query', () => {
    expect(esqlValidator(esqlQuery('FROM logs | LIMIT 10'), {})).toBeNull();
  });

  it('is a no-op for non-ES|QL queries', () => {
    expect(esqlValidator(esqlQuery('FROM logs', { queryType: 'lucene' }), {})).toBeNull();
    expect(esqlValidator(esqlQuery('', { queryType: 'lucene' }), {})).toBeNull();
  });

  it('is a no-op for empty ES|QL queries', () => {
    expect(esqlValidator(esqlQuery(''), {})).toBeNull();
    expect(esqlValidator(esqlQuery(undefined as unknown as string), {})).toBeNull();
  });

  it('returns errors with position info for a broken query', () => {
    const errors = esqlValidator(esqlQuery('FROM logs | WHER x > 1'), {});
    expect(errors).not.toBeNull();
    expect(errors!.length).toBeGreaterThan(0);
    const first = errors![0];
    expect(typeof first.message).toBe('string');
    expect(first.start).toEqual(expect.objectContaining({ line: expect.any(Number), column: expect.any(Number) }));
    expect(first.end).toEqual(expect.objectContaining({ line: expect.any(Number), column: expect.any(Number) }));
  });
});
