import React, { useEffect, useState } from 'react';

import { SelectableValue } from '@grafana/data';
import { AsyncSelect } from '@grafana/ui';

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

  const loadIndices = async () => {
    setIsLoading(true);
    try {
      console.log('IndexSelector: Loading indices...');
      const indexList = await datasource.getIndices();
      console.log('IndexSelector: Received indices:', indexList);

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
    } catch (error) {
      console.error('IndexSelector: Failed to load indices:', error);
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

  useEffect(() => {
    loadIndices();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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
    <AsyncSelect
      value={selectedOption}
      onChange={handleChange}
      loadOptions={async (query: string) => {
        // Filter indices based on query
        if (!query) {
          return indices;
        }
        return indices.filter((opt) => opt.label?.toLowerCase().includes(query.toLowerCase()));
      }}
      defaultOptions={indices}
      placeholder={placeholder || 'Select index or enter custom value'}
      isClearable={true}
      allowCustomValue={true}
      isLoading={isLoading}
      onOpenMenu={loadIndices}
    />
  );
};
