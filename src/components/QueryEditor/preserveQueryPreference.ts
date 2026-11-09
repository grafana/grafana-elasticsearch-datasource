import { store } from '@grafana/data';

/**
 * The "Preserve query" toggle is stored on the query itself so it round-trips with
 * saved dashboards/panels. For a *new* query we still want it to default to the value
 * the user last chose, so we remember that choice per-browser via localStorage and bake
 * it into the query once during `initQuery` (see `preserveQueryReducer`). After that,
 * the query field is the source of truth — we do not re-read localStorage when rendering
 * or switching query types.
 */
const PRESERVE_QUERY_STORAGE_KEY = 'grafana.datasources.elasticsearch.preserveQuery';

export const getPreserveQueryDefault = (): boolean => store.getBool(PRESERVE_QUERY_STORAGE_KEY, false);

export const setPreserveQueryDefault = (value: boolean): void => {
  store.set(PRESERVE_QUERY_STORAGE_KEY, String(value));
};
