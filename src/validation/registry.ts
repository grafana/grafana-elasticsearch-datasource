import { ElasticsearchDataQuery } from '../dataquery.gen';

import { QueryValidator, ValidationContext, ValidationError } from './types';

export class QueryValidatorRegistry {
  private byType = new Map<string, QueryValidator[]>();
  private global: QueryValidator[] = [];

  register(queryType: string, validator: QueryValidator): void {
    const existing = this.byType.get(queryType) ?? [];
    existing.push(validator);
    this.byType.set(queryType, existing);
  }

  registerGlobal(validator: QueryValidator): void {
    this.global.push(validator);
  }

  validate(query: ElasticsearchDataQuery, ctx: ValidationContext): ValidationError[] {
    const errors: ValidationError[] = [];
    for (const validator of this.global) {
      const result = validator(query, ctx);
      if (result) {
        errors.push(...result);
      }
    }
    if (query.queryType) {
      const typed = this.byType.get(query.queryType);
      if (typed) {
        for (const validator of typed) {
          const result = validator(query, ctx);
          if (result) {
            errors.push(...result);
          }
        }
      }
    }
    return errors;
  }
}
