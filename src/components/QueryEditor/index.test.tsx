import { fireEvent, render, screen } from '@testing-library/react';

import { ElasticsearchDataQuery } from '../../dataquery.gen';
import { ElasticDatasource } from '../../datasource';
import React from 'react';

import { QueryEditor } from '.';

const noop = () => void 0;
const datasourceMock = {
  getDatabaseVersion: () => Promise.resolve(null),
} as ElasticDatasource;

describe('QueryEditor', () => {
  describe('Lucene Query Field', () => {
    const buildQuery = (query: string): ElasticsearchDataQuery => ({
      refId: 'A',
      query,
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ id: '2', type: 'date_histogram' }],
    });

    it('renders as a textarea so long queries can word-wrap instead of scrolling horizontally', () => {
      render(<QueryEditor query={buildQuery('')} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      expect(queryField.tagName).toBe('TEXTAREA');
    });

    it('calls onChange with the new value as the user types', () => {
      const onChange = jest.fn<void, [ElasticsearchDataQuery]>();
      render(<QueryEditor query={buildQuery('')} datasource={datasourceMock} onChange={onChange} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      fireEvent.change(queryField, { target: { value: 'status:200' } });

      expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ query: 'status:200' }));
    });

    it('prevents Enter from inserting a literal newline into the query', () => {
      render(<QueryEditor query={buildQuery('status:200')} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      const wasCancelled = !fireEvent(queryField, event);

      expect(wasCancelled).toBe(true);
    });
  });

  describe('Alias Field', () => {
    it('Should correctly render and trigger changes on blur', () => {
      const alias = '{{metric}}';
      const query: ElasticsearchDataQuery = {
        refId: 'A',
        query: '',
        alias,
        metrics: [
          {
            id: '1',
            type: 'count',
          },
        ],
        bucketAggs: [
          {
            type: 'date_histogram',
            id: '2',
          },
        ],
      };

      const onChange = jest.fn<void, [ElasticsearchDataQuery]>();

      render(<QueryEditor query={query} datasource={datasourceMock} onChange={onChange} onRunQuery={noop} />);

      let aliasField = screen.getByLabelText('Alias') as HTMLInputElement;

      // The Query should have an alias field
      expect(aliasField).toBeInTheDocument();

      // its value should match the one in the query
      expect(aliasField.value).toBe(alias);

      // We change value and trigger a blur event to trigger an update
      const newAlias = 'new alias';
      fireEvent.change(aliasField, { target: { value: newAlias } });
      fireEvent.blur(aliasField);

      // the onChange handler should have been called correctly, and the resulting
      // query state should match what expected
      expect(onChange).toHaveBeenCalledTimes(1);
      expect(onChange.mock.calls[0][0].alias).toBe(newAlias);
    });

    it('Should not be shown if last bucket aggregation is not Date Histogram', () => {
      const query: ElasticsearchDataQuery = {
        refId: 'A',
        query: '',
        metrics: [
          {
            id: '1',
            type: 'avg',
          },
        ],
        bucketAggs: [{ id: '2', type: 'terms' }],
      };

      render(<QueryEditor query={query} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      expect(screen.queryByLabelText('Alias')).toBeNull();
    });

    it('Should be shown if last bucket aggregation is Date Histogram', () => {
      const query: ElasticsearchDataQuery = {
        refId: 'A',
        query: '',
        metrics: [
          {
            id: '1',
            type: 'avg',
          },
        ],
        bucketAggs: [{ id: '2', type: 'date_histogram' }],
      };

      render(<QueryEditor query={query} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      expect(screen.getByLabelText('Alias')).toBeEnabled();
    });
  });

  it('Should NOT show Bucket Aggregations Editor if query contains a "singleMetric" metric', () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [
        {
          id: '1',
          type: 'logs',
        },
      ],
      // Even if present, this shouldn't be shown in the UI
      bucketAggs: [{ id: '2', type: 'date_histogram' }],
    };

    render(<QueryEditor query={query} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

    expect(screen.queryByLabelText('Group By')).not.toBeInTheDocument();
  });

  it('Should show Bucket Aggregations Editor if query does NOT contains a "singleMetric" metric', () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [
        {
          id: '1',
          type: 'avg',
        },
      ],
      bucketAggs: [{ id: '2', type: 'date_histogram' }],
    };

    render(<QueryEditor query={query} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

    expect(screen.getByText('Group By')).toBeInTheDocument();
  });

  describe('Include runtime fields toggle', () => {
    it('Should render the toggle and trigger onChange when clicked', () => {
      const query: ElasticsearchDataQuery = {
        refId: 'A',
        query: '',
        metrics: [{ id: '1', type: 'logs' }],
        bucketAggs: [],
      };

      const onChange = jest.fn<void, [ElasticsearchDataQuery]>();
      const onRunQuery = jest.fn();

      render(<QueryEditor query={query} datasource={datasourceMock} onChange={onChange} onRunQuery={onRunQuery} />);

      fireEvent.click(screen.getByText('Options'));

      const toggle = screen.getByLabelText('Include runtime fields');
      expect(toggle).toBeInTheDocument();

      fireEvent.click(toggle);
      expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ includeRuntimeFields: true }));
    });

    it('Should show the toggle even for non-logs queries', () => {
      const query: ElasticsearchDataQuery = {
        refId: 'A',
        query: '',
        metrics: [{ id: '1', type: 'count' }],
        bucketAggs: [{ id: '2', type: 'date_histogram' }],
      };

      render(<QueryEditor query={query} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      fireEvent.click(screen.getByText('Options'));

      expect(screen.getByLabelText('Include runtime fields')).toBeInTheDocument();
    });
  });
});
