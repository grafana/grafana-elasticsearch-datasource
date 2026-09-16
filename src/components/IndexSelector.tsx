import React, { useState, useEffect } from 'react';

import { Alert, Combobox, type ComboboxOption } from '@grafana/ui';

import { useDatasource } from './QueryEditor/ElasticsearchQueryContext';

interface Props {
  value?: string;
  onChange: (value: string | undefined) => void;
  placeholder?: string;
}

const DEFAULT_INDEX_VALUE = '';

export const IndexSelector = ({ value, onChange, placeholder }: Props) => {
  const datasource = useDatasource();
  const [indices, setIndices] = useState<Array<ComboboxOption<string>>>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadIndices = async () => {
      setIsLoading(true);
      setError(null);
      try {
        const indexList = await datasource.getIndices();

        if (indexList.length === 0) {
          setError('No indices found. Check your Elasticsearch connection and permissions.');
        }

        // Add a default option at the top
        const options: Array<ComboboxOption<string>> = [
          {
            label: '(Use datasource default index)',
            value: DEFAULT_INDEX_VALUE,
            description: 'Use the default index pattern from datasource settings',
          },
          ...indexList.map((index: string) => ({
            label: index,
            value: index,
          })),
        ];
        setIndices(options);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : 'Unknown error';
        setError(`Failed to load indices: ${errorMessage}`);
        setIndices([
          {
            label: '(Use datasource default index)',
            value: DEFAULT_INDEX_VALUE,
            description: 'Use the default index pattern from datasource settings',
          },
        ]);
      } finally {
        setIsLoading(false);
      }
    };

    loadIndices();
  }, [datasource]);

  const handleChange = (option: ComboboxOption<string> | null) => {
    // If null, empty string or default value, clear the index
    if (!option || !option.value || option.value === DEFAULT_INDEX_VALUE) {
      onChange(undefined);
    } else {
      onChange(option.value);
    }
  };

  return (
    <>
      {error && <Alert severity="warning" title={error} />}
      <Combobox
        value={value || DEFAULT_INDEX_VALUE}
        onChange={handleChange}
        options={indices}
        placeholder={placeholder || 'Select index or enter custom value'}
        isClearable={true}
        createCustomValue={true}
        loading={isLoading}
      />
    </>
  );
};
