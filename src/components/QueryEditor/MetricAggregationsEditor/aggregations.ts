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

/**
 * Checks if `metric` requires a field (either referring to a document or another aggregation)
 * @param metric
 */
export const isMetricAggregationWithField = (
  metric: BaseMetricAggregation | MetricAggregationWithField
): metric is MetricAggregationWithField => metricAggregationConfig[metric.type].requiresField;

export const isPipelineAggregation = (
  metric: BaseMetricAggregation | PipelineMetricAggregation
): metric is PipelineMetricAggregation => metricAggregationConfig[metric.type].isPipelineAgg;

export const isPipelineAggregationWithMultipleBucketPaths = (
  metric: BaseMetricAggregation | PipelineMetricAggregationWithMultipleBucketPaths
): metric is PipelineMetricAggregationWithMultipleBucketPaths =>
  metricAggregationConfig[metric.type].supportsMultipleBucketPaths;

export const isMetricAggregationWithMissingSupport = (
  metric: BaseMetricAggregation | MetricAggregationWithMissingSupport
): metric is MetricAggregationWithMissingSupport => metricAggregationConfig[metric.type].supportsMissing;

export const isMetricAggregationWithSettings = (
  metric: BaseMetricAggregation | MetricAggregationWithSettings
): metric is MetricAggregationWithSettings => metricAggregationConfig[metric.type].hasSettings;

export const isMetricAggregationWithMeta = (
  metric: BaseMetricAggregation | MetricAggregationWithMeta
): metric is MetricAggregationWithMeta => metricAggregationConfig[metric.type].hasMeta;

export const isMetricAggregationWithInlineScript = (
  metric: BaseMetricAggregation | MetricAggregationWithInlineScript
): metric is MetricAggregationWithInlineScript => metricAggregationConfig[metric.type].supportsInlineScript;

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
