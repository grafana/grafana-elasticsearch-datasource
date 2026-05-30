import { ElasticsearchDataQuery } from '../dataquery.gen';

import { QueryValidatorRegistry } from './registry';
import { QueryValidator } from './types';

const makeQuery = (overrides: Partial<ElasticsearchDataQuery> = {}): ElasticsearchDataQuery => ({
  refId: 'A',
  query: 'test',
  queryType: 'esql',
  ...overrides,
});

describe('QueryValidatorRegistry', () => {
  it('returns empty array when no validators are registered', () => {
    const registry = new QueryValidatorRegistry();
    expect(registry.validate(makeQuery(), {})).toEqual([]);
  });

  it('runs type-scoped validators only for matching queryType', () => {
    const registry = new QueryValidatorRegistry();
    const validator: QueryValidator = jest.fn(() => [{ message: 'bad esql' }]);
    registry.register('esql', validator);

    const esqlErrors = registry.validate(makeQuery({ queryType: 'esql' }), {});
    const luceneErrors = registry.validate(makeQuery({ queryType: 'lucene' }), {});

    expect(esqlErrors).toEqual([{ message: 'bad esql' }]);
    expect(luceneErrors).toEqual([]);
    expect(validator).toHaveBeenCalledTimes(1);
  });

  it('runs global validators for every queryType', () => {
    const registry = new QueryValidatorRegistry();
    const validator: QueryValidator = jest.fn(() => [{ message: 'global rule' }]);
    registry.registerGlobal(validator);

    expect(registry.validate(makeQuery({ queryType: 'esql' }), {})).toEqual([{ message: 'global rule' }]);
    expect(registry.validate(makeQuery({ queryType: 'lucene' }), {})).toEqual([{ message: 'global rule' }]);
    expect(validator).toHaveBeenCalledTimes(2);
  });

  it('runs global validators even when queryType is missing', () => {
    const registry = new QueryValidatorRegistry();
    registry.registerGlobal(() => [{ message: 'runs always' }]);

    const query = makeQuery();
    delete query.queryType;
    expect(registry.validate(query, {})).toEqual([{ message: 'runs always' }]);
  });

  it('runs global validators before typed ones and flattens results', () => {
    const registry = new QueryValidatorRegistry();
    const order: string[] = [];
    registry.registerGlobal((_q) => {
      order.push('global');
      return [{ message: 'g1' }];
    });
    registry.register('esql', (_q) => {
      order.push('typed');
      return [{ message: 't1' }, { message: 't2' }];
    });

    const errors = registry.validate(makeQuery(), {});
    expect(order).toEqual(['global', 'typed']);
    expect(errors).toEqual([{ message: 'g1' }, { message: 't1' }, { message: 't2' }]);
  });

  it('treats null returns as "no errors"', () => {
    const registry = new QueryValidatorRegistry();
    registry.register('esql', () => null);
    expect(registry.validate(makeQuery(), {})).toEqual([]);
  });

  it('passes the context through to validators', () => {
    const registry = new QueryValidatorRegistry();
    const validator: QueryValidator = jest.fn(() => null);
    registry.register('esql', validator);
    registry.validate(makeQuery(), { timeField: '@timestamp' });
    expect(validator).toHaveBeenCalledWith(expect.objectContaining({ queryType: 'esql' }), { timeField: '@timestamp' });
  });

  it('supports multiple validators for the same queryType', () => {
    const registry = new QueryValidatorRegistry();
    registry.register('esql', () => [{ message: 'first' }]);
    registry.register('esql', () => [{ message: 'second' }]);
    expect(registry.validate(makeQuery(), {})).toEqual([{ message: 'first' }, { message: 'second' }]);
  });
});
