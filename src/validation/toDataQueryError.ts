import { DataQueryError } from '@grafana/data';

import { ValidationError } from './types';

const formatError = (error: ValidationError): string => {
  if (error.start) {
    return `[${error.start.line}:${error.start.column}] ${error.message}`;
  }
  return error.message;
};

export function toDataQueryError(refId: string | undefined, errors: ValidationError[]): DataQueryError {
  return {
    refId,
    message: errors.map(formatError).join('\n'),
    status: 400,
  };
}
