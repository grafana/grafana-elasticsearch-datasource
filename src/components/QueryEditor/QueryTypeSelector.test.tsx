import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { ElasticsearchDataQuery } from '../../dataquery.gen';
import { useDispatch } from '../../hooks/useStatelessReducer';
import { renderWithESProvider } from '../../test-helpers/render';

import { changeMetricType } from './MetricAggregationsEditor/state/actions';
import { setPreserveQueryDefault } from './preserveQueryPreference';
import { QueryTypeSelector } from './QueryTypeSelector';
import React from 'react';

jest.mock('../../hooks/useStatelessReducer');

describe('QueryTypeSelector', () => {
  let dispatch: jest.Mock;

  beforeEach(() => {
    dispatch = jest.fn();
    jest.mocked(useDispatch).mockReturnValue(dispatch);
  });

  afterEach(() => {
    jest.clearAllMocks();
    localStorage.clear();
  });

  it('should render radio buttons with correct options', () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    expect(screen.getByRole('radio', { name: 'Metrics' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Logs' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Raw Data' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Raw Document' })).toBeInTheDocument();
  });

  it('should dispatch changeMetricType action when radio button is changed', async () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const logsRadio = screen.getByRole('radio', { name: 'Logs' });
    await userEvent.click(logsRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'logs', previousType: 'count', preserveQuery: false })
    );
  });

  it('should dispatch preserveQuery: true when the query has preserveQuery enabled', async () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
      preserveQuery: true,
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const logsRadio = screen.getByRole('radio', { name: 'Logs' });
    await userEvent.click(logsRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'logs', previousType: 'count', preserveQuery: true })
    );
  });

  it('should not use the sticky localStorage default when the query has no preserveQuery', async () => {
    // Sticky preference only applies when a new query is initialised. Once the editor
    // is rendering, the value on the query model is the source of truth so dashboards
    // behave the same across browsers.
    setPreserveQueryDefault(true);

    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const logsRadio = screen.getByRole('radio', { name: 'Logs' });
    await userEvent.click(logsRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'logs', previousType: 'count', preserveQuery: false })
    );
  });

  it('should convert query type to metric type correctly for raw_data', async () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const rawDataRadio = screen.getByRole('radio', { name: 'Raw Data' });
    await userEvent.click(rawDataRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'raw_data', previousType: 'count', preserveQuery: false })
    );
  });

  it('should convert query type to metric type correctly for raw_document', async () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'count' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const rawDocumentRadio = screen.getByRole('radio', { name: 'Raw Document' });
    await userEvent.click(rawDocumentRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'raw_document', previousType: 'count', preserveQuery: false })
    );
  });

  it('should convert metrics query type to count metric type', async () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [{ id: '1', type: 'logs' }],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    const metricsRadio = screen.getByRole('radio', { name: 'Metrics' });
    await userEvent.click(metricsRadio);

    expect(dispatch).toHaveBeenCalledWith(
      changeMetricType({ id: '1', type: 'count', previousType: 'logs', preserveQuery: false })
    );
  });

  it('should return null when query has no metrics', () => {
    const query: ElasticsearchDataQuery = {
      refId: 'A',
      query: '',
      metrics: [],
      bucketAggs: [{ type: 'date_histogram', id: '2' }],
    };

    const { container } = renderWithESProvider(<QueryTypeSelector />, { providerProps: { query } });

    expect(container.firstChild).toBeNull();
  });
});
