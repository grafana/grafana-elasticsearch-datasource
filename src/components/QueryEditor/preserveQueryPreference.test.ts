import { getPreserveQueryDefault, resolvePreserveQuery, setPreserveQueryDefault } from './preserveQueryPreference';

describe('preserveQueryPreference', () => {
  afterEach(() => {
    localStorage.clear();
  });

  describe('getPreserveQueryDefault', () => {
    it('defaults to false when nothing has been stored', () => {
      expect(getPreserveQueryDefault()).toBe(false);
    });

    it('returns the previously stored preference', () => {
      setPreserveQueryDefault(true);
      expect(getPreserveQueryDefault()).toBe(true);
    });
  });

  describe('resolvePreserveQuery', () => {
    it('returns the explicit per-query value when set, ignoring the stored preference', () => {
      setPreserveQueryDefault(true);
      expect(resolvePreserveQuery(false)).toBe(false);
    });

    it('falls back to the stored preference when the per-query value is undefined', () => {
      setPreserveQueryDefault(true);
      expect(resolvePreserveQuery(undefined)).toBe(true);
    });

    it('falls back to false when nothing is stored and the per-query value is undefined', () => {
      expect(resolvePreserveQuery(undefined)).toBe(false);
    });
  });
});
