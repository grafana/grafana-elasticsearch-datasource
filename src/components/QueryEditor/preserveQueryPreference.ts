import { store } from '@grafana/data';

/**
 * The "Preserve query" toggle is stored on the query itself (so it round-trips with
 * saved dashboards/panels), but for a *new* query we want it to default to the value
 * the user last chose, rather than always resetting to off. We remember that choice
 * per-browser via localStorage, keyed off this constant.
 */
const PRESERVE_QUERY_STORAGE_KEY = 'grafana.datasources.elasticsearch.preserveQuery';

export const getPreserveQueryDefault = (): boolean => store.getBool(PRESERVE_QUERY_STORAGE_KEY, false);

export const setPreserveQueryDefault = (value: boolean): void => {
  store.set(PRESERVE_QUERY_STORAGE_KEY, String(value));
};

/**
 * Resolves the effective `preserveQuery` value for a query: the explicit per-query
 * value when set, otherwise the user's remembered preference.
 */
export const resolvePreserveQuery = (preserveQuery: boolean | undefined): boolean =>
  preserveQuery ?? getPreserveQueryDefault();
