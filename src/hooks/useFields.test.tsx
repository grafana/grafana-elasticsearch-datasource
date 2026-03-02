import { act, renderHook } from '@testing-library/react';
import React, { PropsWithChildren } from 'react';
import { from } from 'rxjs';

import { getDefaultTimeRange, MetricFindValue } from '@grafana/data';

import { ElasticsearchProvider } from '../components/QueryEditor/ElasticsearchQueryContext';
import { ElasticsearchDataQuery, BucketAggregationType, MetricAggregationType } from '../dataquery.gen';
import { ElasticDatasource } from '../datasource';
import { defaultBucketAgg, defaultMetricAgg } from '../queryDef';

import { useFields } from './useFields';

describe('useFields hook', () => {
  // TODO: If we move the field type to the configuration objects as described in the hook's source
  // we can stop testing for getField to be called with the correct parameters.
  it("returns a function that calls datasource's getFields with the correct parameters", async () => {
    const timeRange = getDefaultTimeRange();
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [defaultMetricAgg()],
      bucketAggs: [defaultBucketAgg()],
    };

    const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([[]]));

    const wrapper = ({ children }: PropsWithChildren<{}>) => (
      <ElasticsearchProvider
        datasource={{ getFields } as ElasticDatasource}
        query={query}
        range={timeRange}
        onChange={() => {}}
        onRunQuery={() => {}}
      >
        {children}
      </ElasticsearchProvider>
    );

    //
    // METRIC AGGREGATIONS
    //
    // Cardinality works on every kind of data
    const { result, rerender } = renderHook(
      (aggregationType: BucketAggregationType | MetricAggregationType) => useFields(aggregationType),
      { wrapper, initialProps: 'cardinality' }
    );
    result.current();
    expect(getFields).toHaveBeenLastCalledWith([], timeRange);

    // All other metric aggregations only work on numbers
    rerender('avg');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith(['number'], timeRange);

    //
    // BUCKET AGGREGATIONS
    //
    // Date Histrogram only works on dates
    rerender('date_histogram');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith(['date'], timeRange);

    // Histrogram only works on numbers
    rerender('histogram');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith(['number'], timeRange);

    // Geohash Grid only works on geo_point data
    rerender('geohash_grid');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith(['geo_point'], timeRange);

    // All other bucket aggregation work on any kind of data
    rerender('terms');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith([], timeRange);

    // top_metrics work on only on numeric data in 7.7
    rerender('top_metrics');
    result.current();
    expect(getFields).toHaveBeenLastCalledWith(['number'], timeRange);
  });

  describe('async function', () => {
    const timeRange = getDefaultTimeRange();
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [defaultMetricAgg()],
      bucketAggs: [defaultBucketAgg()],
    };
    const getFieldsMockData: MetricFindValue[] = [
      { text: 'justification_blob.shallow.jsi.sdb.dsel2.bootlegged-gille.botness' },
      { text: 'justification_blob.shallow.jsi.sdb.dsel2.bootlegged-gille.general_algorithm_score' },
      { text: 'justification_blob.shallow.jsi.sdb.dsel2.uncombed-boris.botness' },
      { text: 'justification_blob.shallow.jsi.sdb.dsel2.uncombed-boris.general_algorithm_score' },
      { text: 'overall_vote_score' },
    ];

    it('returns all fields when q is undefined', async () => {
      const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([getFieldsMockData]));

      const wrapper = ({ children }: PropsWithChildren<{}>) => (
        <ElasticsearchProvider
          datasource={{ getFields } as ElasticDatasource}
          query={query}
          range={timeRange}
          onChange={() => {}}
          onRunQuery={() => {}}
        >
          {children}
        </ElasticsearchProvider>
      );

      const { result } = renderHook(() => useFields('avg'), { wrapper });

      let returned!: Awaited<ReturnType<ReturnType<typeof useFields>>>;
      await act(async () => {
        returned = await result.current();
      });

      expect(returned).toHaveLength(getFieldsMockData.length);
      expect(returned.map((o) => o.value)).toEqual(getFieldsMockData.map((f) => f.text));
      expect(returned.every((o, i) => o.label === getFieldsMockData[i].text && o.value === getFieldsMockData[i].text)).toBe(true);
    });

    it('returns only fields that match the filter when q is provided', async () => {
      const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([getFieldsMockData]));

      const wrapper = ({ children }: PropsWithChildren<{}>) => (
        <ElasticsearchProvider
          datasource={{ getFields } as ElasticDatasource}
          query={query}
          range={timeRange}
          onChange={() => {}}
          onRunQuery={() => {}}
        >
          {children}
        </ElasticsearchProvider>
      );

      const { result } = renderHook(() => useFields('avg'), { wrapper });

      let returned!: Awaited<ReturnType<ReturnType<typeof useFields>>>;
      await act(async () => {
        returned = await result.current('vote_score');
      });

      const expected = getFieldsMockData.filter((f) => f.text.includes('vote_score'));
      expect(returned).toHaveLength(expected.length);
      expect(returned.map((o) => o.value)).toEqual(expected.map((f) => f.text));
    });

    it('returns empty array when q matches no field', async () => {
      const getFields: ElasticDatasource['getFields'] = jest.fn(() => from([getFieldsMockData]));

      const wrapper = ({ children }: PropsWithChildren<{}>) => (
        <ElasticsearchProvider
          datasource={{ getFields } as ElasticDatasource}
          query={query}
          range={timeRange}
          onChange={() => {}}
          onRunQuery={() => {}}
        >
          {children}
        </ElasticsearchProvider>
      );

      const { result } = renderHook(() => useFields('avg'), { wrapper });

      let returned!: Awaited<ReturnType<ReturnType<typeof useFields>>>;
      await act(async () => {
        returned = await result.current('another_field');
      });

      expect(returned).toEqual([]);
    });
  });
});
