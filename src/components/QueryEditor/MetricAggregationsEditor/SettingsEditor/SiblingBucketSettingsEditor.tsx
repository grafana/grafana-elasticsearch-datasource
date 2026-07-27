import { css } from '@emotion/css';
import React, { useId } from 'react';

import { InlineField, SegmentAsync, Select } from '@grafana/ui';

import { BaseSiblingPipelineMetricAggregation } from '../../../../dataquery.gen';
import { useFields } from '../../../../hooks/useFields';
import { useDispatch } from '../../../../hooks/useStatelessReducer';
import { SIBLING_INNER_STATS, siblingInnerStatOptions } from '../../../../queryDef';
import { changeMetricSetting } from '../state/actions';

import { SettingField } from './SettingField';

interface Props {
  metric: BaseSiblingPipelineMetricAggregation;
}

export const SiblingBucketSettingsEditor = ({ metric }: Props) => {
  const dispatch = useDispatch();
  const getGroupByOptions = useFields([]);
  const metricFieldId = useId();
  // Show the effective inner stat: query emission falls back to max for
  // unknown values, so the select must not echo an invalid one.
  const requestedStat = metric.settings?.metric ?? '';
  const innerStat = SIBLING_INNER_STATS.includes(requestedStat) ? requestedStat : 'max';

  return (
    <>
      <InlineField
        label="Metric"
        labelWidth={16}
        tooltip="The statistic calculated per group before the results are combined"
        htmlFor={metricFieldId}
      >
        <Select
          id={metricFieldId}
          onChange={(e) => dispatch(changeMetricSetting({ metric, settingName: 'metric', newValue: e.value }))}
          options={siblingInnerStatOptions}
          value={innerStat}
        />
      </InlineField>
      <InlineField
        label="Group By"
        labelWidth={16}
        tooltip="Field whose values define the groups, for example the host name. Required."
        invalid={!metric.settings?.groupBy}
        error="Group By is required"
      >
        <SegmentAsync
          className={css({ marginRight: 0 })}
          loadOptions={getGroupByOptions}
          onChange={(e) => dispatch(changeMetricSetting({ metric, settingName: 'groupBy', newValue: e.value }))}
          placeholder="Select Field"
          value={metric.settings?.groupBy}
        />
      </InlineField>
      <SettingField
        label="Limit"
        metric={metric}
        settingName="limit"
        placeholder="500"
        tooltip="Maximum number of groups (terms size), capped at 65535. Groups beyond the limit are excluded from the result, which undercounts sums."
      />
    </>
  );
};
