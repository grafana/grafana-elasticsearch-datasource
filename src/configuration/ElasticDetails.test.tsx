import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { mockComboboxRect } from '../test/helpers/mockCombobox';

import { ElasticDetails } from './ElasticDetails';
import { createDefaultConfigOptions } from './mocks/configOptions';
import React from 'react';

const selectComboboxOption = async (label: string, optionName: string) => {
  await userEvent.click(screen.getByLabelText(label));
  await userEvent.click(await screen.findByRole('option', { name: optionName }));
};

describe('ElasticDetails', () => {
  beforeAll(() => {
    mockComboboxRect();
  });

  describe('Max concurrent Shard Requests', () => {
    it('should render "Max concurrent Shard Requests" ', () => {
      render(<ElasticDetails onChange={() => {}} value={createDefaultConfigOptions()} />);
      expect(screen.getByLabelText('Max concurrent Shard Requests')).toBeInTheDocument();
    });
  });

  it('should change database on interval change when not set explicitly', async () => {
    const onChangeMock = jest.fn();
    render(<ElasticDetails onChange={onChangeMock} value={createDefaultConfigOptions()} />);

    await selectComboboxOption('Pattern', 'Daily');

    expect(onChangeMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({ interval: 'Daily', index: '[logstash-]YYYY.MM.DD' }),
      })
    );
  });

  it('should change database on interval change if pattern is from example', async () => {
    const onChangeMock = jest.fn();
    const options = createDefaultConfigOptions();
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- exercises the legacy `database` fallback
    options.database = '[logstash-]YYYY.MM.DD.HH';
    render(<ElasticDetails onChange={onChangeMock} value={options} />);

    await selectComboboxOption('Pattern', 'Monthly');

    expect(onChangeMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        jsonData: expect.objectContaining({ interval: 'Monthly', index: '[logstash-]YYYY.MM' }),
      })
    );
  });

  it('should change default query mode when selected', async () => {
    const onChangeMock = jest.fn();
    render(<ElasticDetails onChange={onChangeMock} value={createDefaultConfigOptions()} />);

    await selectComboboxOption('Default query mode', 'Logs');

    expect(onChangeMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ jsonData: expect.objectContaining({ defaultQueryMode: 'logs' }) })
    );
  });
});
