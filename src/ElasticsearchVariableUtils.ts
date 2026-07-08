import { DataFrame, Field, FieldType } from '@grafana/data';

import { ElasticsearchDataQuery } from './dataquery.gen';

export const refId = 'ElasticsearchVariableQueryEditor-VariableQuery';

export type ElasticsearchVariableQuery = ElasticsearchDataQuery;

export const migrateVariableQuery = (rawQuery: string | ElasticsearchDataQuery): ElasticsearchVariableQuery => {
  if (typeof rawQuery !== 'string') {
    return {
      ...rawQuery,
      refId: rawQuery.refId || refId,
      query: rawQuery.query || '',
      meta: rawQuery.meta,
    };
  }

  // Legacy string-based query. This covers the old Grafana-syntax forms
  // ({"find":"terms","field":"..."} and {"find":"fields",...}) as well as plain Lucene strings.
  // All of them route through metricFindQuery() -> getTerms()/getFields() — the same path core
  // Grafana uses (grafana/grafana#120836), which is what this PR extends.
  //
  // We deliberately do NOT translate {"find":"terms"} into a raw DSL query: that path is gated
  // behind the elasticsearchRawDSLQuery backend toggle, produces an empty frame without a metric,
  // and bypasses getTerms() (losing the boolean key_as_string fix, issue #106053). Routing
  // through metricFindQuery() resolves values with no toggle dependency and keeps
  // {"find":"terms"} variables working after externalisation (issue #319).
  return {
    refId,
    query: rawQuery,
    queryType: 'legacy_variable',
    metrics: [{ type: 'raw_document', id: '1' }],
  };
};

export const updateFrame = (frame: DataFrame, meta?: { textField?: string; valueField?: string }): DataFrame => {
  const fields = convertFieldsToVariableFields(frame.fields, meta);
  let length = fields.length > 0 ? fields[0].values.length : frame.length;
  return { ...frame, length, fields };
};

export const convertFieldsToVariableFields = (
  original_fields: Field[],
  meta?: { textField?: string; valueField?: string }
): Field[] => {
  // scenario 1: If no fields found, throw error
  if (original_fields.length < 1) {
    throw new Error('at least one field expected for variable');
  }

  // scenario 2: If meta field found, use and return (at least one text field / value field exist / or first field)
  if (meta?.textField || meta?.valueField) {
    let tf = meta.textField ? original_fields.find((f) => f.name === meta.textField) : undefined;
    let vf = meta.valueField ? original_fields.find((f) => f.name === meta.valueField) : undefined;
    const textField = tf || vf || original_fields[0];
    const valueField = vf || tf || original_fields[0];
    const otherFields = original_fields.filter((f: Field) => f.name !== 'value' && f.name !== 'text');
    return [{ ...textField, name: 'text' }, { ...valueField, name: 'value' }, ...otherFields];
  }

  // scenario 3: If both __text field & __value field found
  let tf = original_fields.find((f) => f.name === '__text');
  let vf = original_fields.find((f) => f.name === '__value');
  if (tf && vf) {
    const otherFields = original_fields.filter((f: Field) => f.name !== '__text' && f.name !== '__value');
    return [
      { ...tf, name: 'text', values: tf.values.map((v) => '' + v) },
      { ...vf, name: 'value', values: vf.values.map((v) => '' + v) },
      ...otherFields,
    ];
  }

  // scenario 4: fallback scenario / legacy scenario where return all fields into a single field.
  let values: string[] = [];
  for (const field of original_fields) {
    for (const value of field.values) {
      if (value !== null && value !== undefined) {
        values.push('' + value);
      }
    }
  }
  return [
    { name: 'text', type: FieldType.string, config: {}, values },
    { name: 'value', type: FieldType.string, config: {}, values },
  ];
};
