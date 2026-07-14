import { MetricAggregation } from '../../../../dataquery.gen';

import { extendedStats, SIBLING_INNER_STATS } from '../../../../queryDef';

const hasValue = (value: string) => (object: { value: string }) => object.value === value;

// FIXME: All the defaults and validations down here should be defined somewhere else
// as they are also the defaults that are gonna be applied to the query.
// In the previous version, the same method was taking care of describing the settings and setting defaults.
export const useDescription = (metric: MetricAggregation): string => {
  switch (metric.type) {
    case 'cardinality': {
      const precisionThreshold = metric.settings?.precision_threshold || '';
      return `Precision threshold: ${precisionThreshold}`;
    }

    case 'percentiles':
      if (metric.settings?.percents && metric.settings?.percents?.length >= 1) {
        return `Values: ${metric.settings?.percents}`;
      }

      return 'Percents: Default';

    case 'extended_stats': {
      const selectedStats = Object.entries(metric.meta || {})
        .map(([key, value]) => value && extendedStats.find(hasValue(key))?.label)
        .filter(Boolean);

      return `Stats: ${selectedStats.length > 0 ? selectedStats.join(', ') : 'None selected'}`;
    }

    case 'raw_document':
    case 'raw_data': {
      const size = metric.settings?.size || 500;
      return `Size: ${size}`;
    }

    case 'sum_bucket':
    case 'max_bucket':
    case 'min_bucket':
    case 'avg_bucket': {
      // Show the effective inner stat: query emission falls back to max for
      // unknown values, so the description must not echo an invalid one.
      const requested = metric.settings?.metric ?? '';
      const inner = SIBLING_INNER_STATS.includes(requested) ? requested : 'max';
      const groupBy = metric.settings?.groupBy || 'not set';
      const limit = metric.settings?.limit || '500';
      return `Metric: ${inner}, Group by: ${groupBy}, Limit: ${limit}`;
    }

    default:
      return 'Options';
  }
};
