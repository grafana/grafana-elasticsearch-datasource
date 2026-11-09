import { css } from '@emotion/css';

import { Combobox, InlineField, MultiCombobox, SegmentAsync } from '@grafana/ui';
import { TopMetrics } from '../../../../dataquery.gen';

import { useFields } from '../../../../hooks/useFields';
import { useDispatch } from '../../../../hooks/useStatelessReducer';
import { orderOptions } from '../../BucketAggregationsEditor/utils';
import { changeMetricSetting } from '../state/actions';
import React from 'react';

interface Props {
  metric: TopMetrics;
}

export const TopMetricsSettingsEditor = ({ metric }: Props) => {
  const dispatch = useDispatch();
  const getOrderByOptions = useFields(['number', 'date']);
  const getMetricsOptions = useFields(metric.type);

  return (
    <>
      <InlineField label="Metrics" labelWidth={16}>
        <MultiCombobox
          onChange={(selected) =>
            dispatch(
              changeMetricSetting({
                metric,
                settingName: 'metrics',
                newValue: selected.map((v) => v.value),
              })
            )
          }
          options={getMetricsOptions}
          value={metric.settings?.metrics}
        />
      </InlineField>
      <InlineField label="Order" labelWidth={16}>
        <Combobox
          onChange={(e) => dispatch(changeMetricSetting({ metric, settingName: 'order', newValue: e.value }))}
          options={orderOptions}
          value={metric.settings?.order}
        />
      </InlineField>
      <InlineField
        label="Order By"
        labelWidth={16}
        className={css({
          '& > div': {
            width: '100%',
          },
        })}
      >
        <SegmentAsync
          className={css({
            marginRight: 0,
          })}
          loadOptions={getOrderByOptions}
          onChange={(e) => dispatch(changeMetricSetting({ metric, settingName: 'orderBy', newValue: e.value }))}
          placeholder="Select Field"
          value={metric.settings?.orderBy}
        />
      </InlineField>
    </>
  );
};
