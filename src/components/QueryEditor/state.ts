import { Action, createAction } from '@reduxjs/toolkit';

import { ElasticsearchDataQuery } from '../../dataquery.gen';
import { QueryType } from '../../types';

import { changeMetricType } from './MetricAggregationsEditor/state/actions';
import { metricAggregationConfig } from './MetricAggregationsEditor/utils';

/**
 * When the `initQuery` Action is dispatched, the query gets populated with default values where values are not present.
 * This means it won't override any existing value in place, but just ensure the query is in a "runnable" state.
 */
export const initQuery = createAction<QueryType | undefined>('init');

export const changeQuery = createAction<ElasticsearchDataQuery['query']>('change_query');

export const changeQueryType = createAction<ElasticsearchDataQuery['queryType']>('change_query_type');

export const changeAliasPattern = createAction<ElasticsearchDataQuery['alias']>('change_alias_pattern');

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

  // Clear the query only when switching between different query modes
  // (e.g. metrics -> logs, or metrics -> raw_data). Switching between two
  // metric aggregations (e.g. Average -> Max) keeps the same implied query
  // type, so we preserve the existing query.
  if (changeMetricType.match(action)) {
    const { type, previousType } = action.payload;
    const impliedQueryTypeChanged =
      previousType === undefined ||
      metricAggregationConfig[type].impliedQueryType !== metricAggregationConfig[previousType].impliedQueryType;

    return impliedQueryTypeChanged ? '' : prevQuery;
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
