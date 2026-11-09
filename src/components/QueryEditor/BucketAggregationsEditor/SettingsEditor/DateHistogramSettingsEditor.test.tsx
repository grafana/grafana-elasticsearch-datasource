import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { DateHistogram } from '../../../../dataquery.gen';

import { useDispatch } from '../../../../hooks/useStatelessReducer';
import { mockComboboxRect } from '../../../../test/helpers/mockCombobox';

import { DateHistogramSettingsEditor } from './DateHistogramSettingsEditor';
import React from 'react';

jest.mock('../../../../hooks/useStatelessReducer');

describe('DateHistogramSettingsEditor', () => {
  beforeAll(() => {
    mockComboboxRect();
  });

  test('Renders the date histogram selector', async () => {
    const bucketAgg: DateHistogram = {
      field: '@timestamp',
      id: '2',
      settings: { interval: 'auto' },
      type: 'date_histogram',
    };
    render(<DateHistogramSettingsEditor bucketAgg={bucketAgg} />);
    expect(await screen.findByText('Fixed interval')).toBeInTheDocument();
    expect(await screen.findByDisplayValue('auto')).toBeInTheDocument();
  });
  test('Renders the date histogram selector with a fixed interval', async () => {
    const bucketAgg: DateHistogram = {
      field: '@timestamp',
      id: '2',
      settings: { interval: '10s' },
      type: 'date_histogram',
    };
    render(<DateHistogramSettingsEditor bucketAgg={bucketAgg} />);
    expect(await screen.findByText('Fixed interval')).toBeInTheDocument();
    expect(await screen.findByDisplayValue('10s')).toBeInTheDocument();
  });
  test('Renders the date histogram selector with a calendar interval', async () => {
    const bucketAgg: DateHistogram = {
      field: '@timestamp',
      id: '2',
      settings: { interval: '1w' },
      type: 'date_histogram',
    };
    render(<DateHistogramSettingsEditor bucketAgg={bucketAgg} />);
    expect(await screen.findByText('Calendar interval')).toBeInTheDocument();
    expect(await screen.findByDisplayValue('1w')).toBeInTheDocument();
  });

  describe('Handling change', () => {
    let dispatch = jest.fn();
    beforeEach(() => {
      dispatch.mockClear();
      jest.mocked(useDispatch).mockReturnValue(dispatch);
    });
    test('Handles changing from calendar to fixed interval type', async () => {
      const bucketAgg: DateHistogram = {
        field: '@timestamp',
        id: '2',
        settings: { interval: '1w' },
        type: 'date_histogram',
      };
      render(<DateHistogramSettingsEditor bucketAgg={bucketAgg} />);

      expect(await screen.findByText('Calendar interval')).toBeInTheDocument();
      expect(await screen.findByDisplayValue('1w')).toBeInTheDocument();

      await userEvent.click(screen.getByLabelText('Calendar interval'));
      await userEvent.click(await screen.findByRole('option', { name: '10s' }));

      expect(dispatch).toHaveBeenCalledTimes(1);
    });
    test('Renders the date histogram selector with a calendar interval', async () => {
      const bucketAgg: DateHistogram = {
        field: '@timestamp',
        id: '2',
        settings: { interval: '1m' },
        type: 'date_histogram',
      };
      render(<DateHistogramSettingsEditor bucketAgg={bucketAgg} />);

      expect(await screen.findByText('Fixed interval')).toBeInTheDocument();
      expect(await screen.findByDisplayValue('1m')).toBeInTheDocument();

      // Type to filter: the options list is virtualised, so distant options
      // are not in the DOM until the list is narrowed down.
      await userEvent.type(screen.getByLabelText('Fixed interval'), '1q');
      await userEvent.click(await screen.findByRole('option', { name: '1q' }));

      expect(dispatch).toHaveBeenCalledTimes(1);
    });
  });
});
