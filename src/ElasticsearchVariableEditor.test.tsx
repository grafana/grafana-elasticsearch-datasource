import React from 'react';
import { act, render, screen, waitFor } from '@testing-library/react';
import { of } from 'rxjs';

import { dateTime, FieldType, LoadingState } from '@grafana/data';

import { ElasticsearchVariableEditor } from './ElasticsearchVariableEditor';
import { ElasticQueryEditorProps, QueryEditor } from './components/QueryEditor';
import { ElasticsearchDataQuery } from './dataquery.gen';
import { ElasticDatasource } from './datasource';

jest.mock('./components/QueryEditor', () => ({
  QueryEditor: jest.fn(({ query }) => <div data-testid="query-editor">Query: {query.query}</div>),
  ElasticQueryEditorProps: {},
}));

describe('ElasticsearchVariableEditor', () => {
  const defaultProps: ElasticQueryEditorProps = {
    query: {
      refId: 'A',
      query: 'test query',
      metrics: [{ type: 'count', id: '1' }],
    },
    onChange: jest.fn(),
    onRunQuery: jest.fn(),
    datasource: {
      query: jest.fn().mockReturnValue(
        of({
          data: [
            {
              fields: [
                { name: 'id', type: FieldType.number, config: {}, values: [1, 2] },
                { name: 'name', type: FieldType.string, config: {}, values: ['a', 'b'] },
                { name: 'status', type: FieldType.string, config: {}, values: ['active', 'inactive'] },
              ],
            },
          ],
        })
      ),
    } as unknown as ElasticDatasource,
    data: undefined,
    range: {
      from: dateTime('2021-01-01'),
      to: dateTime('2021-01-02'),
      raw: { from: 'now-1h', to: 'now' },
    },
  };

  it('should render query editor', () => {
    render(<ElasticsearchVariableEditor {...defaultProps} />);

    expect(screen.getByTestId('query-editor')).toBeInTheDocument();
    expect(screen.getByText('Query: test query')).toBeInTheDocument();
  });

  it('should render field mapping selectors', () => {
    render(<ElasticsearchVariableEditor {...defaultProps} />);

    expect(screen.getByText('Value Field')).toBeInTheDocument();
    expect(screen.getByText('Text Field')).toBeInTheDocument();
  });

  it('should populate field choices from query results', async () => {
    render(<ElasticsearchVariableEditor {...defaultProps} />);

    await waitFor(() => {
      expect(defaultProps.datasource.query).toHaveBeenCalled();
    });
  });

  it('should preserve existing meta values', () => {
    const queryWithMeta: ElasticsearchDataQuery = {
      ...defaultProps.query,
      meta: {
        textField: 'name',
        valueField: 'id',
      },
    };

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, query: queryWithMeta }} />);

    expect(screen.getByDisplayValue('name')).toBeInTheDocument();
    expect(screen.getByDisplayValue('id')).toBeInTheDocument();
  });

  it('should not migrate legacy queries', () => {
    const stringQuery = 'test string query' as unknown as ElasticsearchDataQuery;

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, query: stringQuery }} />);

    expect(screen.getByText('Legacy variable query')).toBeInTheDocument();
    expect(screen.getByDisplayValue('test string query')).toBeInTheDocument();
  });

  it('should handle query errors gracefully', async () => {
    // Mock console.error to suppress error output in tests
    const consoleErrorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});

    const errorObservable = {
      subscribe: (observer: { error: (err: Error) => void }) => {
        setTimeout(() => {
          if (observer.error) {
            observer.error(new Error('Query failed'));
          }
        }, 0);
        return { unsubscribe: jest.fn() };
      },
    };

    const datasourceWithError = {
      ...defaultProps.datasource,
      query: jest.fn().mockReturnValue(errorObservable),
    } as unknown as ElasticDatasource;

    const props = { ...defaultProps, datasource: datasourceWithError };

    // Should not throw error when rendering
    expect(() => render(<ElasticsearchVariableEditor {...props} />)).not.toThrow();

    // Wait for the error to be handled
    await waitFor(() => {
      expect(datasourceWithError.query).toHaveBeenCalled();
    });

    consoleErrorSpy.mockRestore();
  });

  it('should display a query error returned in the response', async () => {
    const datasourceWithError = {
      ...defaultProps.datasource,
      query: jest.fn().mockReturnValue(
        of({ data: [], state: LoadingState.Error, errors: [{ message: 'failed to parse query' }] })
      ),
    } as unknown as ElasticDatasource;

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, datasource: datasourceWithError }} />);

    expect(await screen.findByText('failed to parse query')).toBeInTheDocument();
  });

  it('should display a transport-level query error', async () => {
    const consoleErrorSpy = jest.spyOn(console, 'error').mockImplementation(() => {});
    const errorObservable = {
      subscribe: (observer: { error?: (err: Error) => void }) => {
        setTimeout(() => observer.error?.(new Error('connection refused')), 0);
        return { unsubscribe: jest.fn() };
      },
    };
    const datasourceWithError = {
      ...defaultProps.datasource,
      query: jest.fn().mockReturnValue(errorObservable),
    } as unknown as ElasticDatasource;

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, datasource: datasourceWithError }} />);

    expect(await screen.findByText('connection refused')).toBeInTheDocument();
    consoleErrorSpy.mockRestore();
  });

  it('should clear the error once a subsequent query succeeds', async () => {
    const query = jest
      .fn()
      .mockReturnValueOnce(of({ data: [], state: LoadingState.Error, errors: [{ message: 'boom' }] }))
      .mockReturnValue(
        of({
          data: [{ fields: [{ name: 'name', type: FieldType.string, config: {}, values: ['a'] }] }],
        })
      );
    const ds = { ...defaultProps.datasource, query } as unknown as ElasticDatasource;

    const { rerender } = render(<ElasticsearchVariableEditor {...{ ...defaultProps, datasource: ds }} />);
    expect(await screen.findByText('boom')).toBeInTheDocument();

    // Changing the query content re-runs the preview; the success response must clear the banner.
    rerender(
      <ElasticsearchVariableEditor {...{ ...defaultProps, datasource: ds, query: { ...defaultProps.query, query: 'changed' } }} />
    );
    await waitFor(() => expect(screen.queryByText('boom')).not.toBeInTheDocument());
  });

  it('should run the field-mapping preview over the editor time range, not a hardcoded hour', async () => {
    const customRange = {
      from: dateTime('2020-06-01'),
      to: dateTime('2020-06-08'),
      raw: { from: 'now-7d', to: 'now' },
    };
    const query = jest.fn().mockReturnValue(of({ data: [{ fields: [] }] }));
    const ds = { ...defaultProps.datasource, query } as unknown as ElasticDatasource;

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, datasource: ds, range: customRange }} />);

    await waitFor(() => expect(query).toHaveBeenCalled());
    const request = query.mock.calls[0][0];
    expect(request.range.raw).toEqual({ from: 'now-7d', to: 'now' });
  });

  it('should update query only when query content changes, not meta', () => {
    const { rerender } = render(<ElasticsearchVariableEditor {...defaultProps} />);

    const initialCallCount = (defaultProps.datasource.query as jest.Mock).mock.calls.length;

    // Update meta field only
    const queryWithMeta: ElasticsearchDataQuery = {
      ...defaultProps.query,
      meta: {
        textField: 'name',
      },
    };

    rerender(<ElasticsearchVariableEditor {...defaultProps} query={queryWithMeta} />);

    // Should not trigger new query since only meta changed
    expect((defaultProps.datasource.query as jest.Mock).mock.calls.length).toBe(initialCallCount);
  });

  it('should trigger new query when query content changes', () => {
    const { rerender } = render(<ElasticsearchVariableEditor {...defaultProps} />);

    const initialCallCount = (defaultProps.datasource.query as jest.Mock).mock.calls.length;

    // Update query content
    const updatedQuery: ElasticsearchDataQuery = {
      ...defaultProps.query,
      query: 'updated query',
    };

    rerender(<ElasticsearchVariableEditor {...defaultProps} query={updatedQuery} />);

    // Should trigger new query since query content changed
    expect((defaultProps.datasource.query as jest.Mock).mock.calls.length).toBeGreaterThan(initialCallCount);
  });

  it('should trigger new query when queryType changes', () => {
    const { rerender } = render(<ElasticsearchVariableEditor {...defaultProps} />);

    const initialCallCount = (defaultProps.datasource.query as jest.Mock).mock.calls.length;

    const updatedQuery: ElasticsearchDataQuery = {
      ...defaultProps.query,
      queryType: 'lucene',
    };

    rerender(<ElasticsearchVariableEditor {...defaultProps} query={updatedQuery} />);

    expect((defaultProps.datasource.query as jest.Mock).mock.calls.length).toBeGreaterThan(initialCallCount);
  });

  it('should reset meta fields when QueryEditor fires onChange with a new queryType', () => {
    const onChange = jest.fn();
    const queryWithMeta: ElasticsearchDataQuery = {
      ...defaultProps.query,
      queryType: 'lucene',
      meta: { textField: 'name', valueField: 'id' },
    };

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, query: queryWithMeta, onChange }} />);

    const queryEditorOnChange = (QueryEditor as jest.Mock).mock.calls.at(-1)[0].onChange;
    act(() => queryEditorOnChange({ ...queryWithMeta, queryType: 'dsl' }));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ queryType: 'dsl', meta: undefined }));
  });

  it('should reset meta fields when the metric type tab changes', () => {
    const onChange = jest.fn();
    const queryWithMeta: ElasticsearchDataQuery = {
      ...defaultProps.query,
      metrics: [{ type: 'count', id: '1' }],
      meta: { textField: 'name', valueField: 'id' },
    };

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, query: queryWithMeta, onChange }} />);

    // Simulate the user switching the metric type inside QueryEditor
    const queryEditorOnChange = (QueryEditor as jest.Mock).mock.calls.at(-1)[0].onChange;
    act(() => queryEditorOnChange({ ...queryWithMeta, metrics: [{ type: 'raw_document', id: '1' }] }));

    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ meta: undefined }));
  });

  it('should not reset meta fields when only the query string changes', () => {
    const onChange = jest.fn();
    const queryWithMeta: ElasticsearchDataQuery = {
      ...defaultProps.query,
      meta: { textField: 'name', valueField: 'id' },
    };

    render(<ElasticsearchVariableEditor {...{ ...defaultProps, query: queryWithMeta, onChange }} />);

    const queryEditorOnChange = (QueryEditor as jest.Mock).mock.calls.at(-1)[0].onChange;
    act(() => queryEditorOnChange({ ...queryWithMeta, query: 'updated lucene query' }));

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ query: 'updated lucene query', meta: { textField: 'name', valueField: 'id' } })
    );
  });
});
