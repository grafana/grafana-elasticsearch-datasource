import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import React from 'react';
import { from } from 'rxjs';

import { getDefaultTimeRange, MetricFindValue } from '@grafana/data';
import { ElasticsearchDataQuery, SumBucket } from '../../../../dataquery.gen';

import { ElasticDatasource } from '../../../../datasource';
import { selectOptionInTest } from '../../../../test/helpers/selectOptionInTest';
import { ElasticsearchProvider } from '../../ElasticsearchQueryContext';

import { SettingsEditor } from '.';

describe('SiblingBucketSettingsEditor', () => {
  const metric: SumBucket = {
    id: '1',
    type: 'sum_bucket',
    field: 'storage_used',
    settings: { metric: 'max', groupBy: 'host', limit: '500' },
  };

  const query: ElasticsearchDataQuery = {
    refId: 'A',
    query: '',
    metrics: [metric],
    bucketAggs: [{ type: 'date_histogram', id: '2' }],
  };

  const datasource = {
    getFields: jest.fn(() => from([[]])),
  } as unknown as ElasticDatasource;

  it('renders the sibling settings and dispatches limit changes', () => {
    const onChange = jest.fn();

    render(
      <ElasticsearchProvider
        query={query}
        datasource={datasource}
        onChange={onChange}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metric} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    // Open the collapsed settings section, whose label is the row description.
    fireEvent.click(screen.getByRole('button', { name: /Metric: max, Group by: host, Limit: 500/i }));

    expect(screen.getByText('Group By')).toBeInTheDocument();

    const limitInput = screen.getByLabelText('Limit');
    fireEvent.change(limitInput, { target: { value: '1000' } });
    fireEvent.blur(limitInput);

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        metrics: [expect.objectContaining({ settings: expect.objectContaining({ limit: '1000' }) })],
      })
    );
  });

  it('shows the required error when Group By is not set', () => {
    const metricWithoutGroupBy: SumBucket = {
      id: '1',
      type: 'sum_bucket',
      field: 'storage_used',
      settings: { metric: 'max', limit: '500' },
    };

    render(
      <ElasticsearchProvider
        query={{ ...query, metrics: [metricWithoutGroupBy] }}
        datasource={datasource}
        onChange={() => {}}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metricWithoutGroupBy} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Metric: max, Group by: not set, Limit: 500/i }));

    expect(screen.getByText('Group By is required')).toBeInTheDocument();
  });

  it('falls back to defaults in the description when settings are empty', () => {
    const metricWithoutSettings: SumBucket = {
      id: '1',
      type: 'sum_bucket',
      field: 'storage_used',
    };

    render(
      <ElasticsearchProvider
        query={{ ...query, metrics: [metricWithoutSettings] }}
        datasource={datasource}
        onChange={() => {}}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metricWithoutSettings} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    expect(screen.getByRole('button', { name: /Metric: max, Group by: not set, Limit: 500/i })).toBeInTheDocument();
  });

  it('shows the effective inner stat when the query model holds an invalid one', () => {
    // Query emission falls back to max for unknown inner stats, so the
    // description and select must not echo the invalid value.
    const metricWithInvalidStat: SumBucket = {
      id: '1',
      type: 'sum_bucket',
      field: 'storage_used',
      settings: { metric: 'cardinality', groupBy: 'host', limit: '500' },
    };

    render(
      <ElasticsearchProvider
        query={{ ...query, metrics: [metricWithInvalidStat] }}
        datasource={datasource}
        onChange={() => {}}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metricWithInvalidStat} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    const settingsButton = screen.getByRole('button', { name: /Metric: max, Group by: host, Limit: 500/i });
    fireEvent.click(settingsButton);

    expect(screen.queryByText('cardinality')).not.toBeInTheDocument();
    expect(screen.getByText('Max')).toBeInTheDocument();
  });

  it('dispatches a metric setting change when a different inner stat is selected', async () => {
    const onChange = jest.fn();

    render(
      <ElasticsearchProvider
        query={query}
        datasource={datasource}
        onChange={onChange}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metric} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Metric: max, Group by: host, Limit: 500/i }));

    // The Metric row hosts the only combobox in the open panel (Group By is a SegmentAsync).
    // getByLabelText cannot be used as the Select id lands on a wrapper div, not the input.
    await selectOptionInTest(screen.getByRole('combobox'), 'Sum');

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        metrics: [expect.objectContaining({ settings: expect.objectContaining({ metric: 'sum' }) })],
      })
    );
  });

  it('dispatches a groupBy setting change when a field is selected via the Group By picker', async () => {
    const onChange = jest.fn();
    const metricWithoutGroupBy: SumBucket = {
      id: '1',
      type: 'sum_bucket',
      field: 'storage_used',
      settings: { metric: 'max', limit: '500' },
    };
    const fieldAwareDatasource = {
      getFields: jest.fn(() => from([[{ text: 'host' }] as MetricFindValue[]])),
    } as unknown as ElasticDatasource;

    render(
      <ElasticsearchProvider
        query={{ ...query, metrics: [metricWithoutGroupBy] }}
        datasource={fieldAwareDatasource}
        onChange={onChange}
        onRunQuery={() => {}}
        range={getDefaultTimeRange()}
      >
        <SettingsEditor metric={metricWithoutGroupBy} previousMetrics={[]} />
      </ElasticsearchProvider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Metric: max, Group by: not set, Limit: 500/i }));

    await userEvent.click(screen.getByText('Select Field'));
    await userEvent.click(await screen.findByText('host'));

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({
        metrics: [expect.objectContaining({ settings: expect.objectContaining({ groupBy: 'host' }) })],
      })
    );
  });
});
