import { getPreserveQueryDefault, setPreserveQueryDefault } from './preserveQueryPreference';

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
});
