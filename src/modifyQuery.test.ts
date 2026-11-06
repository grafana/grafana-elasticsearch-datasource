import {
  addAdHocFiltersToEsqlQuery,
  addFilterToQuery,
  addStringFilterToQuery,
  queryHasFilter,
  removeFilterFromQuery,
} from './modifyQuery';

describe('queryHasFilter', () => {
  it('should return true if the query contains the positive filter', () => {
    expect(queryHasFilter('label:"value"', 'label', 'value')).toBe(true);
    expect(queryHasFilter('label: "value"', 'label', 'value')).toBe(true);
    expect(queryHasFilter('label : "value"', 'label', 'value')).toBe(true);
    expect(queryHasFilter('label:value', 'label', 'value')).toBe(true);
    expect(queryHasFilter('this:"that" AND label:value', 'label', 'value')).toBe(true);
    expect(queryHasFilter('this:"that" OR (test:test AND label:value)', 'label', 'value')).toBe(true);
    expect(queryHasFilter('this:"that" OR (test:test AND label:value)', 'test', 'test')).toBe(true);
    expect(queryHasFilter('(this:"that" OR test:test) AND label:value', 'this', 'that')).toBe(true);
    expect(queryHasFilter('(this:"that" OR test:test) AND label:value', 'test', 'test')).toBe(true);
    expect(queryHasFilter('(this:"that" OR test :test) AND label:value', 'test', 'test')).toBe(true);
    expect(
      queryHasFilter(
        'message:"Jun 20 17:19:47 Xtorm syslogd[348]: ASL Sender Statistics"',
        'message',
        'Jun 20 17:19:47 Xtorm syslogd[348]: ASL Sender Statistics'
      )
    ).toBe(true);
  });
  it('should return false if the query does not contain the positive filter', () => {
    expect(queryHasFilter('label:"value"', 'label', 'otherValue')).toBe(false);
    expect(queryHasFilter('-label:"value"', 'label', 'value')).toBe(false);
  });
  it('should return true if the query contains the negative filter', () => {
    expect(queryHasFilter('-label:"value"', 'label', 'value', '-')).toBe(true);
    expect(queryHasFilter('-label: "value"', 'label', 'value', '-')).toBe(true);
    expect(queryHasFilter('-label : "value"', 'label', 'value', '-')).toBe(true);
    expect(queryHasFilter('-label:value', 'label', 'value', '-')).toBe(true);
    expect(queryHasFilter('this:"that" AND -label:value', 'label', 'value', '-')).toBe(true);
  });
  it('should return false if the query does not contain the negative filter', () => {
    expect(queryHasFilter('label:"value"', 'label', 'otherValue', '-')).toBe(false);
    expect(queryHasFilter('label:"value"', 'label', 'value', '-')).toBe(false);
  });
  it('should support filters containing colons', () => {
    expect(queryHasFilter('label\\:name:"value"', 'label:name', 'value')).toBe(true);
    expect(queryHasFilter('-label\\:name:"value"', 'label:name', 'value', '-')).toBe(true);
  });
  it('should support filters containing quotes', () => {
    expect(queryHasFilter('label\\:name:"some \\"value\\""', 'label:name', 'some "value"')).toBe(true);
    expect(queryHasFilter('-label\\:name:"some \\"value\\""', 'label:name', 'some "value"', '-')).toBe(true);
  });
});

describe('addFilterToQuery', () => {
  it('should add a positive filter to the query', () => {
    expect(addFilterToQuery('', 'label', 'value')).toBe('label:"value"');
  });
  it('should add a positive filter to the query with other filters', () => {
    expect(addFilterToQuery('label2:"value2"', 'label', 'value')).toBe('label2:"value2" AND label:"value"');
  });
  it('should add a negative filter to the query', () => {
    expect(addFilterToQuery('', 'label', 'value', '-')).toBe('-label:"value"');
  });
  it('should add a negative filter to the query with other filters', () => {
    expect(addFilterToQuery('label2:"value2"', 'label', 'value', '-')).toBe('label2:"value2" AND -label:"value"');
  });
  it('should support filters with colons', () => {
    expect(addFilterToQuery('', 'label:name', 'value')).toBe('label\\:name:"value"');
  });
  it('should support filters with quotes', () => {
    expect(addFilterToQuery('', 'label:name', 'the "value"')).toBe('label\\:name:"the \\"value\\""');
  });
});

describe('removeFilterFromQuery', () => {
  it('should remove filter from query', () => {
    expect(removeFilterFromQuery('label:"value"', 'label', 'value')).toBe('');
  });
  it('should remove filter from query with other filters', () => {
    expect(removeFilterFromQuery('label:"value" AND label2:"value2"', 'label', 'value')).toBe('label2:"value2"');
    expect(removeFilterFromQuery('label:value AND label2:"value2"', 'label', 'value')).toBe('label2:"value2"');
    expect(removeFilterFromQuery('label : "value" OR label2:"value2"', 'label', 'value')).toBe('label2:"value2"');
    expect(removeFilterFromQuery('test:"test" OR label:"value" AND label2:"value2"', 'label', 'value')).toBe(
      'test:"test" OR label2:"value2"'
    );
    expect(removeFilterFromQuery('test:"test" OR (label:"value" AND label2:"value2")', 'label', 'value')).toBe(
      'test:"test" OR label2:"value2"'
    );
    expect(removeFilterFromQuery('(test:"test" OR label:"value") AND label2:"value2"', 'label', 'value')).toBe(
      '(test:"test") AND label2:"value2"'
    );
    expect(removeFilterFromQuery('(test:"test" OR label:"value") AND label2:"value2"', 'test', 'test')).toBe(
      'label:"value" AND label2:"value2"'
    );
    expect(removeFilterFromQuery('test:"test" OR (label:"value" AND label2:"value2")', 'label2', 'value2')).toBe(
      'test:"test" OR (label:"value")'
    );
  });
  it('should not remove the wrong filter', () => {
    expect(removeFilterFromQuery('-label:"value" AND label2:"value2"', 'label', 'value')).toBe(
      '-label:"value" AND label2:"value2"'
    );
    expect(removeFilterFromQuery('label2:"value2" OR -label:value', 'label', 'value')).toBe(
      'label2:"value2" OR -label:value'
    );
    expect(removeFilterFromQuery('-label : "value" OR label2:"value2"', 'label', 'value')).toBe(
      '-label : "value" OR label2:"value2"'
    );
  });
  it('should support filters with colons', () => {
    expect(removeFilterFromQuery('label\\:name:"value"', 'label:name', 'value')).toBe('');
  });
  it('should support filters with quotes', () => {
    expect(removeFilterFromQuery('label\\:name:"the \\"value\\""', 'label:name', 'the "value"')).toBe('');
  });
});

describe('addStringFilterToQuery', () => {
  it('should add a positive filter to a query', () => {
    expect(addStringFilterToQuery('label:"value"', 'filter')).toBe('label:"value" AND "filter"');
    expect(addStringFilterToQuery('', 'filter')).toBe('"filter"');
    expect(addStringFilterToQuery(' ', 'filter')).toBe('"filter"');
  });

  it('should add a negative filter to a query', () => {
    expect(addStringFilterToQuery('label:"value"', 'filter', false)).toBe('label:"value" NOT "filter"');
    expect(addStringFilterToQuery('', 'filter', false)).toBe('NOT "filter"');
    expect(addStringFilterToQuery(' ', 'filter', false)).toBe('NOT "filter"');
  });

  it('should escape filter values', () => {
    expect(addStringFilterToQuery('label:"value"', '"filter"')).toBe('label:"value" AND "\\"filter\\""');
    expect(addStringFilterToQuery('label:"value"', '"filter"', false)).toBe('label:"value" NOT "\\"filter\\""');
  });

  it('should escape filter values with backslashes', () => {
    expect(addStringFilterToQuery('label:"value"', '"filter with \\"')).toBe(
      'label:"value" AND "\\"filter with \\\\\\""'
    );
    expect(addStringFilterToQuery('label:"value"', '"filter with \\"', false)).toBe(
      'label:"value" NOT "\\"filter with \\\\\\""'
    );
  });
});

describe('addAdHocFiltersToEsqlQuery', () => {
  const rowQuery = 'ROW id=[1,2,3,4,5], __line__="test", @timestamp=NOW() | MV_EXPAND id';

  it('inserts an equality filter after ROW and before MV_EXPAND', () => {
    const query = addAdHocFiltersToEsqlQuery(rowQuery, [{ key: 'id', operator: '=', value: '2', condition: '' }]);
    expect(query).not.toContain('AND id:"2"');
    expect(query).toMatch(/ROW[\s\S]*WHERE id == 2[\s\S]*MV_EXPAND id/);
  });

  it.each([
    ['=', 'id == 2'],
    ['!=', 'id != 2'],
    ['<', 'id < 2'],
    ['>', 'id > 2'],
    ['<=', 'id <= 2'],
    ['>=', 'id >= 2'],
    ['=~', '__line__ RLIKE "test[24]"'],
    ['!~', '__line__ NOT RLIKE "test[24]"'],
  ])('translates %s into ES|QL', (operator, expected) => {
    const key = operator === '=~' || operator === '!~' ? '__line__' : 'id';
    const value = operator === '=~' || operator === '!~' ? 'test[24]' : '2';
    const query = addAdHocFiltersToEsqlQuery('FROM access | STATS COUNT()', [
      { key, operator, value, condition: '' },
    ]);
    expect(query).toContain(expected);
    expect(query.indexOf('WHERE')).toBeLessThan(query.indexOf('STATS'));
  });

  it('inserts the filter immediately after FROM, before STATS', () => {
    const query = addAdHocFiltersToEsqlQuery('FROM access | STATS COUNT() BY app | LIMIT 10000', [
      { key: 'response_status', operator: '=', value: '504', condition: '' },
    ]);
    expect(query).toMatch(/FROM access[\s\S]*WHERE response_status == 504[\s\S]*STATS COUNT\(\)/);
  });

  it('quotes string values and escapes quotes', () => {
    const query = addAdHocFiltersToEsqlQuery('FROM access', [
      { key: 'status', operator: '=', value: 'say "hi"', condition: '' },
    ]);
    expect(query).toContain('status == "say \\"hi\\""');
  });

  it('quotes field names that are not plain identifiers', () => {
    const query = addAdHocFiltersToEsqlQuery('FROM access', [
      { key: 'field:name', operator: '=', value: 'x', condition: '' },
    ]);
    expect(query).toContain('`field:name` == "x"');
  });

  it('renames the level key to the configured log level field', () => {
    const query = addAdHocFiltersToEsqlQuery(
      'FROM access',
      [{ key: 'level', operator: '=', value: 'error', condition: '' }],
      'level_field'
    );
    expect(query).toContain('level_field == "error"');
  });

  it('leaves the query unchanged when filters are empty or invalid', () => {
    expect(addAdHocFiltersToEsqlQuery(rowQuery, [])).toBe(rowQuery);
    expect(addAdHocFiltersToEsqlQuery(rowQuery, [{ key: '', operator: '=', value: '2', condition: '' }])).toBe(rowQuery);
    expect(addAdHocFiltersToEsqlQuery('}{}{[', [{ key: 'id', operator: '=', value: '2', condition: '' }])).toBe('}{}{[');
  });
});
