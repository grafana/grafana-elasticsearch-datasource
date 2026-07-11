import { fireEvent, render, screen } from '@testing-library/react';
import React from 'react';
import { from } from 'rxjs';

import { getDefaultTimeRange } from '@grafana/data';
import { ElasticsearchDataQuery, SumBucket } from '../../../../dataquery.gen';

import { ElasticDatasource } from '../../../../datasource';
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
});
