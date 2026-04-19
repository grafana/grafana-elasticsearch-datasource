import { ElasticsearchDataQuery } from '../dataquery.gen';

export interface ValidationPosition {
  line: number;
  column: number;
}

export interface ValidationError {
  message: string;
  start?: ValidationPosition;
  end?: ValidationPosition;
}

export interface ValidationContext {
  timeField?: string;
}

export type QueryValidator = (
  query: ElasticsearchDataQuery,
  ctx: ValidationContext
) => ValidationError[] | null;
