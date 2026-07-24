import { Action, createAction } from '@reduxjs/toolkit';

import { ElasticsearchDataQuery } from '../../dataquery.gen';
import { QueryType } from '../../types';

import { changeMetricType } from './MetricAggregationsEditor/state/actions';

import { metricAggregationConfig } from './MetricAggregationsEditor/utils';
import { getPreserveQueryDefault } from './preserveQueryPreference';

/**
 * When the `initQuery` Action is dispatched, the query gets populated with default values where values are not present.
 * This means it won't override any existing value in place, but just ensure the query is in a "runnable" state.
 */
export const initQuery = createAction<QueryType | undefined>('init');

export const changeQuery = createAction<ElasticsearchDataQuery['query']>('change_query');

export const changeQueryType = createAction<ElasticsearchDataQuery['queryType']>('change_query_type');

export const changeAliasPattern = createAction<ElasticsearchDataQuery['alias']>('change_alias_pattern');

export const changeIndex = createAction<string | undefined>('change_index');

export const changeEditorType = createAction<ElasticsearchDataQuery['editorType']>('change_editor_type');

export const changeEditorTypeAndResetQuery = createAction<{
  editorType: ElasticsearchDataQuery['editorType'];
  queryType: ElasticsearchDataQuery['queryType'];
}>('change_editor_type_and_reset_query');

export const queryReducer = (prevQuery: ElasticsearchDataQuery['query'], action: Action) => {
  if (changeQuery.match(action)) {
    return action.payload;
  }

  if (changeEditorTypeAndResetQuery.match(action)) {
    return '';
  }

  // Clear query when switching query types (e.g., from Lucene to DSL or vice versa)
  if (changeQueryType.match(action)) {
    return '';
  }

  // Clear the query only when the metric change alters the implied query type
  // (e.g. metrics -> logs, or -> raw_data). Switching between aggregations that
  // share the same implied query type (e.g. count -> avg) preserves the query.
  // The user can opt in to always preserving the query via the `preserveQuery` flag on the action payload.
  // See https://github.com/grafana/grafana-elasticsearch-datasource/issues/309
  // See https://github.com/grafana/grafana-elasticsearch-datasource/issues/350
  if (changeMetricType.match(action)) {
    const { previousType, type, preserveQuery } = action.payload;

    if (preserveQuery) {
      return prevQuery;
    }
    const previousImpliedQueryType = previousType ? metricAggregationConfig[previousType].impliedQueryType : undefined;
    const nextImpliedQueryType = metricAggregationConfig[type].impliedQueryType;

    // only wipe the query when the *kind* of query changed (e.g. metrics -> logs)
    if (previousImpliedQueryType !== nextImpliedQueryType) {
      return '';
    }

    return prevQuery;
  }

  if (initQuery.match(action)) {
    return prevQuery || '';
  }

  return prevQuery;
};

export const queryTypeReducer = (prevQueryType: ElasticsearchDataQuery['queryType'], action: Action) => {
  if (changeQueryType.match(action)) {
    return action.payload;
  }

  if (changeEditorTypeAndResetQuery.match(action)) {
    // When switching editor types, set queryType accordingly:
    // - 'code' editor uses DSL queries
    // - 'builder' editor uses Lucene queries
    return action.payload.editorType === 'code' ? action.payload.queryType : 'lucene';
  }

  if (changeEditorType.match(action)) {
    return action.payload === 'builder' ? 'lucene' : 'dsl';
  }

  if (initQuery.match(action)) {
    return prevQueryType || 'lucene';
  }

  return prevQueryType;
};

export const aliasPatternReducer = (prevAliasPattern: ElasticsearchDataQuery['alias'], action: Action) => {
  if (changeAliasPattern.match(action)) {
    return action.payload;
  }

  if (changeEditorTypeAndResetQuery.match(action)) {
    return '';
  }

  if (initQuery.match(action)) {
    return prevAliasPattern || '';
  }

  return prevAliasPattern;
};

export const editorTypeReducer = (prevEditorType: ElasticsearchDataQuery['editorType'], action: Action) => {
  if (changeEditorType.match(action)) {
    return action.payload;
  }

  if (changeEditorTypeAndResetQuery.match(action)) {
    return action.payload.editorType;
  }

  if (initQuery.match(action)) {
    return prevEditorType || 'builder';
  }

  return prevEditorType;
};

export const indexReducer = (prevIndex: string | undefined, action: Action) => {
  if (changeIndex.match(action)) {
    return action.payload;
  }

  if (changeEditorTypeAndResetQuery.match(action)) {
    return undefined;
  }

  if (initQuery.match(action)) {
    return prevIndex;
  }

  return prevIndex;
};

/**
 * Bake the remembered "Preserve query" preference into the query once on init so the
 * toggle state always round-trips through the saved query JSON, instead of being
 * resolved from localStorage at read time (which would make the same dashboard behave
 * differently across browsers).
 */
export const preserveQueryReducer = (
  prevPreserveQuery: ElasticsearchDataQuery['preserveQuery'],
  action: Action
): ElasticsearchDataQuery['preserveQuery'] => {
  if (initQuery.match(action)) {
    return prevPreserveQuery ?? getPreserveQueryDefault();
  }

  return prevPreserveQuery;
};
