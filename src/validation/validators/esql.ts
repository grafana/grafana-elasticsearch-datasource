import { Parser } from '@elastic/esql';

import { QueryValidator, ValidationError } from '../types';

export const esqlValidator: QueryValidator = (query) => {
  if (query.queryType !== 'esql') {
    return null;
  }
  const src = query.query;
  if (!src) {
    return null;
  }

  let errors: ValidationError[] = [];
  try {
    const result = Parser.parse(src, { withFormatting: true });
    errors = result.errors.map((err) => ({
      message: err.message,
      start: { line: err.startLineNumber, column: err.startColumn },
      end: { line: err.endLineNumber, column: err.endColumn },
    }));
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    errors = [{ message: `Failed to parse ES|QL query: ${message}` }];
  }

  return errors.length ? errors : null;
};
