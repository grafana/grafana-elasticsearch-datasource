import { dateTime, getLocale, setLocale, toUtc } from '@grafana/data';

import { IndexPattern } from './IndexPattern';

describe('IndexPattern', () => {
  const originalLocale = getLocale();
  afterEach(() => setLocale(originalLocale));

  describe('when getting index for today', () => {
    test('should return correct index name', () => {
      const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
      const expected = 'asd-' + toUtc().format('YYYY.MM.DD');

      expect(pattern.getIndexForToday()).toBe(expected);
    });

    test('should format date using western arabic numerals regardless of locale', () => {
      setLocale('ar_SA'); // saudi-arabic, formatting for YYYY.MM.DD looks like "٢٠٢٠.٠٩.٠٣"
      const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
      const expected = 'asd-' + toUtc().locale('en').format('YYYY.MM.DD');
      expect(pattern.getIndexForToday()).toBe(expected);
    });
  });

  describe('when getting index list for time range', () => {
    describe('no interval', () => {
      test('should return correct index', () => {
        const pattern = new IndexPattern('my-metrics');
        const from = dateTime(new Date(2015, 4, 30, 1, 2, 3));
        const to = dateTime(new Date(2015, 5, 1, 12, 5, 6));
        expect(pattern.getIndexList(from, to)).toEqual('my-metrics');
      });
    });

    describe('daily', () => {
      test('should return correct index list', () => {
        const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
        const from = dateTime(new Date(1432940523000));
        const to = dateTime(new Date(1433153106000));

        const expected = ['asd-2015.05.29', 'asd-2015.05.30', 'asd-2015.05.31', 'asd-2015.06.01'];

        expect(pattern.getIndexList(from, to)).toEqual(expected);
      });

      test('should format date using western arabic numerals regardless of locale', () => {
        setLocale('ar_SA'); // saudi-arabic, formatting for YYYY.MM.DD looks like "٢٠٢٠.٠٩.٠٣"
        const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
        const from = dateTime(new Date(1432940523000));
        const to = dateTime(new Date(1433153106000));

        const expected = ['asd-2015.05.29', 'asd-2015.05.30', 'asd-2015.05.31', 'asd-2015.06.01'];

        expect(pattern.getIndexList(from, to)).toEqual(expected);
      });
    });

    describe('weekly', () => {
      it('should return correct index list', () => {
        const pattern = new IndexPattern('[asd-]YYYY.WW', 'Weekly');
        // Sunday, February 21, 2021 1:00:00 AM
        const from = dateTime(new Date(1613869200000));
        // Friday, March 5, 2021 1:00:00 AM
        const to = dateTime(new Date(1614906000000));

        const expected = ['asd-2021.07', 'asd-2021.08', 'asd-2021.09'];

        expect(pattern.getIndexList(from, to)).toEqual(expected);
      });
    });

    // Regression coverage for https://github.com/grafana/grafana/issues/123095:
    // hourly patterns with a 12-hour `hh` token must resolve against UTC,
    // regardless of the host or browser timezone. A panel time range covering
    // UTC midnight on Apr 20 (which is 08:00 of Apr 21 in UTC+8) must produce
    // `2026.04.20.12` (UTC midnight in 12-hour clock), not `2026.04.21.12`
    // (the local-time date).
    describe('hourly', () => {
      it('resolves index date to UTC when the pattern uses 12-hour `hh` (issue #123095)', () => {
        const pattern = new IndexPattern('[nginx_access-]YYYY.MM.DD.hh', 'Hourly');
        const from = dateTime('2026-04-20T00:00:00Z');
        const to = dateTime('2026-04-20T00:01:00Z');

        expect(pattern.getIndexList(from, to)).toEqual(['nginx_access-2026.04.20.12']);
      });

      it('resolves index date to UTC when the pattern uses 24-hour `HH`', () => {
        const pattern = new IndexPattern('[nginx_access-]YYYY.MM.DD.HH', 'Hourly');
        const from = dateTime('2026-04-20T00:00:00Z');
        const to = dateTime('2026-04-20T03:00:00Z');

        expect(pattern.getIndexList(from, to)).toEqual([
          'nginx_access-2026.04.20.00',
          'nginx_access-2026.04.20.01',
          'nginx_access-2026.04.20.02',
          'nginx_access-2026.04.20.03',
        ]);
      });
    });
  });

  describe('when getting index list from single date', () => {
    it('Should return index matching the starting time and subsequent ones', () => {
      const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
      const from = dateTime(new Date(1432940523000));

      const expected = [
        'asd-2015.05.29',
        'asd-2015.05.30',
        'asd-2015.05.31',
        'asd-2015.06.01',
        'asd-2015.06.02',
        'asd-2015.06.03',
        'asd-2015.06.04',
        'asd-2015.06.05',
      ];

      expect(pattern.getIndexList(from)).toEqual(expected);
    });

    it('Should return index matching the starting time and previous ones', () => {
      const pattern = new IndexPattern('[asd-]YYYY.MM.DD', 'Daily');
      const to = dateTime(new Date(1432940523000));

      const expected = [
        'asd-2015.05.22',
        'asd-2015.05.23',
        'asd-2015.05.24',
        'asd-2015.05.25',
        'asd-2015.05.26',
        'asd-2015.05.27',
        'asd-2015.05.28',
        'asd-2015.05.29',
      ];

      expect(pattern.getIndexList(undefined, to)).toEqual(expected);
    });
  });
});
