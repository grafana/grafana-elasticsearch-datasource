import { Button } from '@grafana/ui';

import { MetricAggregation } from '../../../dataquery.gen';
import { useDispatch } from '../../../hooks/useStatelessReducer';
import { impliedQueryType } from '../../../utils';
import { useQuery } from '../ElasticsearchQueryContext';
import { QueryEditorRow } from '../QueryEditorRow';

import { MetricEditor } from './MetricEditor';
import { addMetric, removeMetric, toggleMetricVisibility } from './state/actions';
import React from 'react';

interface Props {
  nextId: MetricAggregation['id'];
}

export const MetricAggregationsEditor = ({ nextId }: Props) => {
  const dispatch = useDispatch();
  const { metrics } = useQuery();
  const totalMetrics = metrics?.length || 0;

  return (
    <>
      {metrics?.map((metric, index) => {
        if (metric.type !== 'logs' && metric.type !== 'raw_data' && metric.type !== 'raw_document') {
          return (
            <QueryEditorRow
              key={`${metric.type}-${metric.id}`}
              label={`Metric (${metric.id})`}
              hidden={metric.hide}
              onHideClick={() => dispatch(toggleMetricVisibility(metric.id))}
              onRemoveClick={totalMetrics > 1 && (() => dispatch(removeMetric(metric.id)))}
            >
              <MetricEditor value={metric} />

              {impliedQueryType(metric.type) === 'metrics' && index === 0 && (
                <Button
                  variant="secondary"
                  fill="text"
                  icon="plus"
                  onClick={() => dispatch(addMetric(nextId))}
                  tooltip="Add metric"
                  aria-label="Add metric"
                />
              )}
            </QueryEditorRow>
          );
        }
        return null;
      })}
    </>
  );
};
