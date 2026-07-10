import {
  BaseMetricAggregation,
  MetricAggregationType,
  MetricAggregationWithField,
  MetricAggregationWithInlineScript,
  MetricAggregationWithMissingSupport,
  MetricAggregationWithSettings,
  PipelineMetricAggregation,
  PipelineMetricAggregationWithMultipleBucketPaths,
} from '../../../dataquery.gen';
import { MetricAggregationWithMeta } from '../../../types';

import { metricAggregationConfig } from './utils';

// Guards
// Given the structure of the aggregations (ie. `settings` field being always optional) we cannot
// determine types based solely on objects' properties, therefore we use `metricAggregationConfig` as the
// source of truth.
// Saved queries can carry aggregation types that no longer exist (e.g. `moving_avg`), so these lookups
// must not assume a config entry is present and fall back to `false` rather than throwing.

/**
 * Checks if `metric` requires a field (either referring to a document or another aggregation)
 * @param metric
 */
export const isMetricAggregationWithField = (
  metric: BaseMetricAggregation | MetricAggregationWithField
): metric is MetricAggregationWithField => metricAggregationConfig[metric.type]?.requiresField ?? false;

export const isPipelineAggregation = (
  metric: BaseMetricAggregation | PipelineMetricAggregation
): metric is PipelineMetricAggregation => metricAggregationConfig[metric.type]?.isPipelineAgg ?? false;

export const isPipelineAggregationWithMultipleBucketPaths = (
  metric: BaseMetricAggregation | PipelineMetricAggregationWithMultipleBucketPaths
): metric is PipelineMetricAggregationWithMultipleBucketPaths =>
  metricAggregationConfig[metric.type]?.supportsMultipleBucketPaths ?? false;

export const isMetricAggregationWithMissingSupport = (
  metric: BaseMetricAggregation | MetricAggregationWithMissingSupport
): metric is MetricAggregationWithMissingSupport => metricAggregationConfig[metric.type]?.supportsMissing ?? false;

export const isMetricAggregationWithSettings = (
  metric: BaseMetricAggregation | MetricAggregationWithSettings
): metric is MetricAggregationWithSettings => metricAggregationConfig[metric.type]?.hasSettings ?? false;

export const isMetricAggregationWithMeta = (
  metric: BaseMetricAggregation | MetricAggregationWithMeta
): metric is MetricAggregationWithMeta => metricAggregationConfig[metric.type]?.hasMeta ?? false;

export const isMetricAggregationWithInlineScript = (
  metric: BaseMetricAggregation | MetricAggregationWithInlineScript
): metric is MetricAggregationWithInlineScript => metricAggregationConfig[metric.type]?.supportsInlineScript ?? false;

export const METRIC_AGGREGATION_TYPES: MetricAggregationType[] = [
  'count',
  'avg',
  'sum',
  'min',
  'max',
  'extended_stats',
  'percentiles',
  'cardinality',
  'raw_document',
  'raw_data',
  'logs',
  'moving_fn',
  'derivative',
  'serial_diff',
  'cumulative_sum',
  'bucket_script',
  'rate',
  'top_metrics',
];

export const isMetricAggregationType = (s: MetricAggregationType | string): s is MetricAggregationType =>
  METRIC_AGGREGATION_TYPES.includes(s as MetricAggregationType);
