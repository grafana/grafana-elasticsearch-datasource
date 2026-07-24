import React, { useState } from 'react';

import { SelectableValue } from '@grafana/data';
import { Alert, Select } from '@grafana/ui';

import { useDatasource } from './QueryEditor/ElasticsearchQueryContext';

interface Props {
  value?: string;
  onChange: (value: string | undefined) => void;
  placeholder?: string;
}

export const IndexSelector = ({ value, onChange, placeholder }: Props) => {
  const datasource = useDatasource();
  const [indices, setIndices] = useState<Array<SelectableValue<string>>>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadIndices = async () => {
    setIsLoading(true);
    setError(null);
    try {
      const indexList = await datasource.getIndices();

      if (indexList.length === 0) {
        setError('No indices found. Check your Elasticsearch connection and permissions.');
      }

      // Add a default option at the top
      const options: Array<SelectableValue<string>> = [
        {
          label: '(Use datasource default index)',
          value: undefined,
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
          value: undefined,
          description: 'Use the default index pattern from datasource settings',
        },
      ]);
    } finally {
      setIsLoading(false);
    }
  };


  const handleChange = (option: SelectableValue<string> | null) => {
    // If null or explicitly undefined value, clear the index
    if (!option || option.value === undefined) {
      onChange(undefined);
    } else {
      onChange(option.value);
    }
  };

  // Determine the selected option
  const selectedOption = value
    ? indices.find((opt) => opt.value === value) || { label: value, value: value }
    : indices.find((opt) => opt.value === undefined);

  return (
    <>
      {error && <Alert severity="warning" title={error} />}
      <Select
        value={selectedOption}
        onChange={handleChange}
        options={indices}
        placeholder={placeholder || 'Select index or enter custom value'}
        isClearable={true}
        allowCustomValue={true}
        isLoading={isLoading}
        onOpenMenu={loadIndices}
      />
    </>
  );
};
