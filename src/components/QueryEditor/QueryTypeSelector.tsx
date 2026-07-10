import { RadioButtonGroup } from '@grafana/ui';

import { QUERY_TYPE_SELECTOR_OPTIONS } from '../../configuration/utils';
import { useDispatch } from '../../hooks/useStatelessReducer';
import { queryTypeToMetricType } from '../../queryDef';
import { QueryType } from '../../types';

import { useQuery } from './ElasticsearchQueryContext';
import { changeMetricType } from './MetricAggregationsEditor/state/actions';
import { metricAggregationConfig } from './MetricAggregationsEditor/utils';
import React from 'react';

export const QueryTypeSelector = () => {
  const query = useQuery();
  const dispatch = useDispatch();

  const firstMetric = query.metrics?.[0];

  if (firstMetric == null) {
    // not sure if this can really happen, but we should handle it anyway
    return null;
  }

  // Removed metric types (e.g. `moving_avg`) implied `metrics`; fall back to that
  // so a saved query using a removed type doesn't crash the type picker.
  const queryType = metricAggregationConfig[firstMetric.type]?.impliedQueryType ?? 'metrics';

  const onChange = (newQueryType: QueryType) => {
    dispatch(
      changeMetricType({
        id: firstMetric.id,
        type: queryTypeToMetricType(newQueryType),
        previousType: firstMetric.type,
        preserveQuery: query.preserveQuery ?? false,
      })
    );
  };

  return (
    <RadioButtonGroup<QueryType>
      fullWidth={false}
      options={QUERY_TYPE_SELECTOR_OPTIONS}
      value={queryType}
      onChange={onChange}
    />
  );
};
