import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import React, { PropsWithChildren, act } from 'react';
import { from } from 'rxjs';
import { SemVer } from 'semver';

import { getDefaultTimeRange } from '@grafana/data';

import { Average, Count, ElasticsearchDataQuery, UniqueCount } from '../../../dataquery.gen';
import { ElasticDatasource } from '../../../datasource';
import { defaultBucketAgg } from '../../../queryDef';
import { ElasticsearchProvider } from '../ElasticsearchQueryContext';

import { MetricEditor } from './MetricEditor';

describe('Metric Editor', () => {
  it('Should display a "None" option for "field" if the metric supports inline script', async () => {
    const avg: Average = {
      id: '1',
      type: 'avg',
    };

    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [avg],
      bucketAggs: [defaultBucketAgg('2')],
    };

    const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([[]]));

    const wrapper = ({ children }: PropsWithChildren<{}>) => (
      <ElasticsearchProvider
        datasource={{ getFields } as ElasticDatasource}
        query={query}
        range={getDefaultTimeRange()}
        onChange={() => {}}
        onRunQuery={() => {}}
      >
        {children}
      </ElasticsearchProvider>
    );

    render(<MetricEditor value={avg} />, { wrapper });

    act(() => {
      fireEvent.click(screen.getByText('Select Field'));
    });

    expect(await screen.findByText('None')).toBeInTheDocument();
  });

  it('Should not display a "None" option for "field" if the metric does not support inline script', async () => {
    const avg: UniqueCount = {
      id: '1',
      type: 'cardinality',
    };

    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [avg],
      bucketAggs: [defaultBucketAgg('2')],
    };

    const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([[]]));

    const wrapper = ({ children }: PropsWithChildren<{}>) => (
      <ElasticsearchProvider
        datasource={{ getFields } as ElasticDatasource}
        query={query}
        range={getDefaultTimeRange()}
        onChange={() => {}}
        onRunQuery={() => {}}
      >
        {children}
      </ElasticsearchProvider>
    );

    render(<MetricEditor value={avg} />, { wrapper });

    act(() => {
      fireEvent.click(screen.getByText('Select Field'));
    });

    expect(await screen.findByText('No options found')).toBeInTheDocument();
    expect(screen.queryByText('None')).not.toBeInTheDocument();
  });

  it('Should not list special metrics', async () => {
    const count: Count = {
      id: '1',
      type: 'count',
    };

    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [count],
      bucketAggs: [],
    };

    const getDatabaseVersion: ElasticDatasource['getDatabaseVersion'] = jest.fn(() => Promise.resolve(null));

    const wrapper = ({ children }: PropsWithChildren<{}>) => (
      <ElasticsearchProvider
        datasource={{ getDatabaseVersion } as ElasticDatasource}
        query={query}
        range={getDefaultTimeRange()}
        onChange={() => {}}
        onRunQuery={() => {}}
      >
        {children}
      </ElasticsearchProvider>
    );

    render(<MetricEditor value={count} />, { wrapper });

    await userEvent.click(screen.getByText('Count'));

    // we check if the list-of-options is visible by
    // checking for an item to exist
    expect(await screen.findByText('Extended Stats')).toBeInTheDocument();

    // now we make sure the should-not-be-shown items are not shown
    expect(screen.queryByText('Logs')).toBeNull();
    expect(screen.queryByText('Raw Data')).toBeNull();
    expect(screen.queryByText('Raw Document (deprecated)')).toBeNull();
  });

  // Regression test for https://github.com/grafana/grafana/issues/115127:
  // SemVer ranges exclude prereleases by default, so versions like
  // `8.14.0-SNAPSHOT` reported by snapshot builds of Elasticsearch were
  // filtering every metric type out of the picker.
  it('Should list metric types for prerelease Elasticsearch versions (e.g. -SNAPSHOT)', async () => {
    const count: Count = { id: '1', type: 'count' };

    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [count],
      bucketAggs: [],
    };

    const getDatabaseVersion: ElasticDatasource['getDatabaseVersion'] = jest.fn(() =>
      Promise.resolve(new SemVer('8.14.0-SNAPSHOT'))
    );

    const wrapper = ({ children }: PropsWithChildren<{}>) => (
      <ElasticsearchProvider
        datasource={{ getDatabaseVersion } as ElasticDatasource}
        query={query}
        range={getDefaultTimeRange()}
        onChange={() => {}}
        onRunQuery={() => {}}
      >
        {children}
      </ElasticsearchProvider>
    );

    render(<MetricEditor value={count} />, { wrapper });

    await userEvent.click(screen.getByText('Count'));

    // Base metric types must remain visible for prerelease versions.
    expect(await screen.findByText('Average')).toBeInTheDocument();
    expect(screen.getByText('Extended Stats')).toBeInTheDocument();

    // Version-gated `moving_avg` (`<8.0.0`) must still be excluded — the fix
    // should not regress upper-bound filtering when the version is a prerelease.
    expect(screen.queryByText('Moving Average')).toBeNull();
  });
});
