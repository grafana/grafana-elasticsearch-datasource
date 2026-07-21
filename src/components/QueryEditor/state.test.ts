import { ElasticsearchDataQuery } from '../../dataquery.gen';
import { reducerTester } from '../reducerTester';

import { changeMetricType } from './MetricAggregationsEditor/state/actions';
import {
  aliasPatternReducer,
  changeAliasPattern,
  changeEditorTypeAndResetQuery,
  changeIndex,
  changeQuery,
  changeQueryType,
  indexReducer,
  initQuery,
  queryReducer,
  queryTypeReducer,
} from './state';

describe('Query Reducer', () => {
  describe('On Init', () => {
    it('Should maintain the previous `query` if present', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'Some lucene query';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(initQuery())
        .thenStateShouldEqual(initialQuery);
    });

    it('Should set an empty `query` if it is not already set', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = undefined;
      const expectedQuery = '';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(initQuery())
        .thenStateShouldEqual(expectedQuery);
    });
  });

  it('Should correctly set `query`', () => {
    const expectedQuery: ElasticsearchDataQuery['query'] = 'Some lucene query';

    reducerTester<ElasticsearchDataQuery['query']>()
      .givenReducer(queryReducer, '')
      .whenActionIsDispatched(changeQuery(expectedQuery))
      .thenStateShouldEqual(expectedQuery);
  });

  it('Should not change state with other action types', () => {
    const initialState: ElasticsearchDataQuery['query'] = 'Some lucene query';

    reducerTester<ElasticsearchDataQuery['query']>()
      .givenReducer(queryReducer, initialState)
      .whenActionIsDispatched({ type: 'THIS ACTION SHOULD NOT HAVE ANY EFFECT IN THIS REDUCER' })
      .thenStateShouldEqual(initialState);
  });

  describe('When switching editor type', () => {
    it('Should clear query when switching editor types', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'Some lucene query';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'code', queryType: 'dsl' }))
        .thenStateShouldEqual('');
    });
  });

  describe('When switching query type', () => {
    it('Should clear query when switching from lucene to dsl', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'field:value';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeQueryType('dsl'))
        .thenStateShouldEqual('');
    });

    it('Should clear query when switching from dsl to lucene', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = '{"query": {"match_all": {}}}';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeQueryType('lucene'))
        .thenStateShouldEqual('');
    });
  });

  describe('When switching metric type', () => {
    it('Should clear query when switching from logs to metrics', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = '{"query": {"match_all": {}}}';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeMetricType({ id: '1', type: 'avg', previousType: 'logs' }))
        .thenStateShouldEqual('');
    });

    it('Should clear query when switching to raw_data', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'field:value';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeMetricType({ id: '1', type: 'raw_data', previousType: 'avg' }))
        .thenStateShouldEqual('');
    });

    it('Should preserve query when switching between metric aggregations (count -> avg)', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'field:value';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeMetricType({ id: '1', type: 'avg', previousType: 'count' }))
        .thenStateShouldEqual(initialQuery);
    });

    it('Should clear query when no previousType is provided', () => {
      const initialQuery: ElasticsearchDataQuery['query'] = 'field:value';

      reducerTester<ElasticsearchDataQuery['query']>()
        .givenReducer(queryReducer, initialQuery)
        .whenActionIsDispatched(changeMetricType({ id: '1', type: 'avg' }))
        .thenStateShouldEqual('');
    });
  });
});

describe('Alias Pattern Reducer', () => {
  it('Should correctly set `alias`', () => {
    const expectedAlias: ElasticsearchDataQuery['alias'] = 'Some alias pattern';

    reducerTester<ElasticsearchDataQuery['query']>()
      .givenReducer(aliasPatternReducer, '')
      .whenActionIsDispatched(changeAliasPattern(expectedAlias))
      .thenStateShouldEqual(expectedAlias);
  });

  it('Should not change state with other action types', () => {
    const initialState: ElasticsearchDataQuery['alias'] = 'Some alias pattern';

    reducerTester<ElasticsearchDataQuery['query']>()
      .givenReducer(aliasPatternReducer, initialState)
      .whenActionIsDispatched({ type: 'THIS ACTION SHOULD NOT HAVE ANY EFFECT IN THIS REDUCER' })
      .thenStateShouldEqual(initialState);
  });

  describe('When switching editor type', () => {
    it('Should clear alias when switching editor types', () => {
      const initialAlias: ElasticsearchDataQuery['alias'] = 'Some alias pattern';

      reducerTester<ElasticsearchDataQuery['alias']>()
        .givenReducer(aliasPatternReducer, initialAlias)
        .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'code', queryType: 'dsl' }))
        .thenStateShouldEqual('');
    });
  });
});

describe('Query Type Reducer', () => {
  it('Should correctly set queryType', () => {
    const expectedQueryType: ElasticsearchDataQuery['queryType'] = 'dsl';

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, 'lucene')
      .whenActionIsDispatched(changeQueryType(expectedQueryType))
      .thenStateShouldEqual(expectedQueryType);
  });

  it('Should set to lucene when switching to builder editor', () => {
    const initialQueryType: ElasticsearchDataQuery['queryType'] = 'dsl';

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, initialQueryType)
      .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'builder', queryType: 'lucene' }))
      .thenStateShouldEqual('lucene');
  });

  it('Should set to dsl when switching to code editor', () => {
    const initialQueryType: ElasticsearchDataQuery['queryType'] = 'lucene';

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, initialQueryType)
      .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'code', queryType: 'dsl' }))
      .thenStateShouldEqual('dsl');
  });

  it('Should set to esql when switching to code editor', () => {
    const initialQueryType: ElasticsearchDataQuery['queryType'] = 'lucene';

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, initialQueryType)
      .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'code', queryType: 'esql' }))
      .thenStateShouldEqual('esql');
  });

  it('Should default to lucene on init if not set', () => {
    const initialQueryType: ElasticsearchDataQuery['queryType'] = undefined;

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, initialQueryType)
      .whenActionIsDispatched(initQuery())
      .thenStateShouldEqual('lucene');
  });

  it('Should maintain queryType on init if already set', () => {
    const initialQueryType: ElasticsearchDataQuery['queryType'] = 'dsl';

    reducerTester<ElasticsearchDataQuery['queryType']>()
      .givenReducer(queryTypeReducer, initialQueryType)
      .whenActionIsDispatched(initQuery())
      .thenStateShouldEqual('dsl');
  });
});

describe('Index Reducer', () => {
  it('Should correctly set index', () => {
    const expectedIndex = 'logs-*';

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, undefined)
      .whenActionIsDispatched(changeIndex(expectedIndex))
      .thenStateShouldEqual(expectedIndex);
  });

  it('Should correctly clear index when set to undefined', () => {
    const initialIndex = 'logs-*';

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, initialIndex)
      .whenActionIsDispatched(changeIndex(undefined))
      .thenStateShouldEqual(undefined);
  });

  it('Should maintain index on init if already set', () => {
    const initialIndex = 'logs-*';

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, initialIndex)
      .whenActionIsDispatched(initQuery())
      .thenStateShouldEqual(initialIndex);
  });

  it('Should maintain undefined index on init if not set', () => {
    const initialIndex = undefined;

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, initialIndex)
      .whenActionIsDispatched(initQuery())
      .thenStateShouldEqual(undefined);
  });

  it('Should reset index to undefined when switching editor types', () => {
    const initialIndex = 'logs-*';

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, initialIndex)
      .whenActionIsDispatched(changeEditorTypeAndResetQuery({ editorType: 'code', queryType: 'dsl' }))
      .thenStateShouldEqual(undefined);
  });

  it('Should not change state with other action types', () => {
    const initialIndex = 'logs-*';

    reducerTester<string | undefined>()
      .givenReducer(indexReducer, initialIndex)
      .whenActionIsDispatched({ type: 'THIS ACTION SHOULD NOT HAVE ANY EFFECT IN THIS REDUCER' })
      .thenStateShouldEqual(initialIndex);
  });
});
