import { act, fireEvent, render, screen } from '@testing-library/react';

import { createTheme } from '@grafana/data';

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

    it('renders in a monospace font, matching the Code editor and the query editors of other datasources', () => {
      // Regression test: the Lucene box briefly rendered in the default UI sans-serif
      // font after the Slate-based QueryField (which got monospace for free via a
      // global grafana-ui CSS rule) was replaced with a plain Input/TextArea. See
      // https://github.com/grafana/grafana-elasticsearch-datasource/pull/310 and #349.
      //
      // Assert against the theme's configured monospace font rather than a hardcoded
      // font name, so this doesn't break if the theme's font choice ever changes.
      render(<QueryEditor query={buildQuery('')} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      const expectedFont = createTheme().typography.fontFamilyMonospace.replace(/['"]/g, '').split(',')[0].trim();
      expect(getComputedStyle(queryField).fontFamily.replace(/['"]/g, '')).toContain(expectedFont);
    });

    it('calls onChange with the new value as the user types', () => {
      const onChange = jest.fn<void, [ElasticsearchDataQuery]>();
      render(<QueryEditor query={buildQuery('')} datasource={datasourceMock} onChange={onChange} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      fireEvent.change(queryField, { target: { value: 'status:200' } });

      expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ query: 'status:200' }));
    });

    it('strips newlines from the query regardless of input path (typing Enter, paste, drag-and-drop)', () => {
      const onChange = jest.fn<void, [ElasticsearchDataQuery]>();
      render(<QueryEditor query={buildQuery('')} datasource={datasourceMock} onChange={onChange} onRunQuery={noop} />);

      const queryField = screen.getByPlaceholderText('Enter a lucene query');
      // A textarea preserves embedded newlines in its value on change, no matter how
      // they got there — paste and drag-and-drop surface exactly like this.
      fireEvent.change(queryField, { target: { value: 'status:200\nAND level:error\nAND host:foo' } });

      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({ query: 'status:200 AND level:error AND host:foo' })
      );
    });

    it('accounts for the textarea border when growing to fit content, so the last line is not clipped', () => {
      const { rerender } = render(
        <QueryEditor query={buildQuery('status:200')} datasource={datasourceMock} onChange={noop} onRunQuery={noop} />
      );
      const queryField = screen.getByPlaceholderText('Enter a lucene query') as HTMLTextAreaElement;

      // jsdom doesn't perform real layout, so scrollHeight/clientHeight/offsetHeight are
      // always 0 unless stubbed. Simulate a bordered element: 100px of content+padding
      // (scrollHeight/clientHeight), plus a 1px top/bottom border that scrollHeight excludes.
      Object.defineProperty(queryField, 'scrollHeight', { value: 100, configurable: true });
      Object.defineProperty(queryField, 'clientHeight', { value: 100, configurable: true });
      Object.defineProperty(queryField, 'offsetHeight', { value: 102, configurable: true });

      rerender(
        <QueryEditor
          query={buildQuery('status:200 AND level:error')}
          datasource={datasourceMock}
          onChange={noop}
          onRunQuery={noop}
        />
      );

      expect(queryField.style.height).toBe('102px');
    });

    describe('resize-driven re-measurement', () => {
      let observedCallback: ResizeObserverCallback | undefined;
      const originalResizeObserver = global.ResizeObserver;

      beforeEach(() => {
        observedCallback = undefined;
        class MockResizeObserver {
          constructor(callback: ResizeObserverCallback) {
            observedCallback = callback;
          }
          observe = jest.fn();
          unobserve = jest.fn();
          disconnect = jest.fn();
        }
        global.ResizeObserver = MockResizeObserver as unknown as typeof ResizeObserver;
      });

      afterEach(() => {
        global.ResizeObserver = originalResizeObserver;
      });

      it('re-measures height when the field width changes, but ignores same-width notifications', () => {
        render(
          <QueryEditor
            query={buildQuery('status:200 AND level:error')}
            datasource={datasourceMock}
            onChange={noop}
            onRunQuery={noop}
          />
        );
        const queryField = screen.getByPlaceholderText('Enter a lucene query') as HTMLTextAreaElement;
        expect(observedCallback).toBeDefined();

        Object.defineProperty(queryField, 'scrollHeight', { value: 50, configurable: true });
        Object.defineProperty(queryField, 'clientHeight', { value: 50, configurable: true });
        Object.defineProperty(queryField, 'offsetHeight', { value: 52, configurable: true });

        // Width goes from unset to 400px (e.g. Explore split-pane settling on mount): re-measure.
        act(() => {
          observedCallback!([{ contentRect: { width: 400 } } as ResizeObserverEntry], {} as ResizeObserver);
        });
        expect(queryField.style.height).toBe('52px');

        // Content wrap points would differ at a new size; simulate the geometry
        // a real browser would report if the field had re-wrapped.
        Object.defineProperty(queryField, 'scrollHeight', { value: 90, configurable: true });
        Object.defineProperty(queryField, 'clientHeight', { value: 90, configurable: true });
        Object.defineProperty(queryField, 'offsetHeight', { value: 92, configurable: true });

        // Same width reported again (e.g. a height-only notification caused by our own
        // height update): must be ignored, so the height should stay unchanged.
        act(() => {
          observedCallback!([{ contentRect: { width: 400 } } as ResizeObserverEntry], {} as ResizeObserver);
        });
        expect(queryField.style.height).toBe('52px');

        // Width actually changes (e.g. dragging the Explore split-pane divider): re-measure
        // and pick up the new content geometry.
        act(() => {
          observedCallback!([{ contentRect: { width: 300 } } as ResizeObserverEntry], {} as ResizeObserver);
        });
        expect(queryField.style.height).toBe('92px');
      });
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
