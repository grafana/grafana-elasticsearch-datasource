import { isSiblingPipelineAggregation } from './components/QueryEditor/MetricAggregationsEditor/aggregations';
import { clampSiblingBucketLimit, isPipelineAgg, isPipelineAggWithMultipleBucketPaths, queryTypeToMetricType } from './queryDef';
import type { QueryType } from './types';

describe('ElasticQueryDef', () => {
  describe('isPipelineMetric', () => {
    describe('moving_avg', () => {
      const result = isPipelineAgg('moving_avg');

      test('is pipe line metric', () => {
        expect(result).toBe(true);
      });
    });

    describe('count', () => {
      const result = isPipelineAgg('count');

      test('is not pipe line metric', () => {
        expect(result).toBe(false);
      });
    });
  });

  describe('isPipelineAggWithMultipleBucketPaths', () => {
    describe('bucket_script', () => {
      const result = isPipelineAggWithMultipleBucketPaths('bucket_script');

      test('should have multiple bucket paths support', () => {
        expect(result).toBe(true);
      });
    });

    describe('moving_avg', () => {
      const result = isPipelineAggWithMultipleBucketPaths('moving_avg');

      test('should not have multiple bucket paths support', () => {
        expect(result).toBe(false);
      });
    });
  });

  describe('queryTypeToMetricType', () => {
    describe('when type is undefined', () => {
      test('should return count as default', () => {
        const result = queryTypeToMetricType(undefined);
        expect(result).toBe('count');
      });
    });

    describe('when type is metrics', () => {
      test('should return count', () => {
        const result = queryTypeToMetricType('metrics' as QueryType);
        expect(result).toBe('count');
      });
    });

    describe('when type is logs', () => {
      test('should return logs', () => {
        const result = queryTypeToMetricType('logs' as QueryType);
        expect(result).toBe('logs');
      });
    });

    describe('when type is raw_data', () => {
      test('should return raw_data', () => {
        const result = queryTypeToMetricType('raw_data' as QueryType);
        expect(result).toBe('raw_data');
      });
    });

    describe('when type is raw_document', () => {
      test('should return raw_document', () => {
        const result = queryTypeToMetricType('raw_document' as QueryType);
        expect(result).toBe('raw_document');
      });
    });

    describe('when type is invalid', () => {
      test('should throw an error', () => {
        expect(() => {
          queryTypeToMetricType('invalid_type' as QueryType);
        }).toThrow('invalid query type: invalid_type');
      });
    });
  });

  describe('clampSiblingBucketLimit', () => {
    test('defaults to 500 when unset or invalid', () => {
      expect(clampSiblingBucketLimit(undefined)).toBe(500);
      expect(clampSiblingBucketLimit('')).toBe(500);
      expect(clampSiblingBucketLimit('abc')).toBe(500);
      expect(clampSiblingBucketLimit('0')).toBe(500);
      expect(clampSiblingBucketLimit('-5')).toBe(500);
    });

    test('rejects partial numeric strings for parity with the backend strconv.Atoi', () => {
      expect(clampSiblingBucketLimit('500abc')).toBe(500);
      expect(clampSiblingBucketLimit('1e3')).toBe(500);
      expect(clampSiblingBucketLimit('50.5')).toBe(500);
    });

    test('passes through valid values and clamps to the elasticsearch maximum', () => {
      expect(clampSiblingBucketLimit('50')).toBe(50);
      expect(clampSiblingBucketLimit('65535')).toBe(65535);
      expect(clampSiblingBucketLimit('999999')).toBe(65535);
    });
  });

  describe('isSiblingPipelineAggregation', () => {
    test('true for the four sibling bucket types', () => {
      for (const type of ['sum_bucket', 'max_bucket', 'min_bucket', 'avg_bucket'] as const) {
        expect(isSiblingPipelineAggregation({ id: '1', type })).toBe(true);
      }
    });

    test('false for parent pipeline and plain metrics', () => {
      expect(isSiblingPipelineAggregation({ id: '1', type: 'derivative' })).toBe(false);
      expect(isSiblingPipelineAggregation({ id: '1', type: 'avg' })).toBe(false);
    });
  });
});
