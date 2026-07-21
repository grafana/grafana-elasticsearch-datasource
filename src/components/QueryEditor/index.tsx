import { css } from '@emotion/css';
import React, { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { SemVer } from 'semver';

import { getDefaultTimeRange, GrafanaTheme2, QueryEditorProps } from '@grafana/data';
import { Alert, ConfirmModal, InlineField, InlineLabel, Input, TextArea, useStyles2 } from '@grafana/ui';

import { ElasticsearchDataQuery, QueryType } from '../../dataquery.gen';
import { useNextId } from '../../hooks/useNextId';
import { useDispatch } from '../../hooks/useStatelessReducer';
import { EditorType, ElasticDatasourceLike, ElasticsearchOptions } from '../../types';
import { isSupportedVersion, isTimeSeriesQuery, unsupportedVersionMessage } from '../../utils';

import { BucketAggregationsEditor } from './BucketAggregationsEditor';
import { CodeEditorSection } from './CodeEditorSection';
import { EditorTypeSelector } from './EditorTypeSelector';
import { ElasticsearchProvider } from './ElasticsearchQueryContext';
import { ElasticsearchQueryOptions } from './ElasticsearchQueryOptions';
import { MetricAggregationsEditor } from './MetricAggregationsEditor';
import { metricAggregationConfig } from './MetricAggregationsEditor/utils';
import { QueryTypeSelector } from './QueryTypeSelector';
import { changeAliasPattern, changeEditorTypeAndResetQuery, changeQuery } from './state';

export type ElasticQueryEditorProps = QueryEditorProps<
  ElasticDatasourceLike,
  ElasticsearchDataQuery,
  ElasticsearchOptions
>;

// a react hook that returns the elasticsearch database version,
// or `null`, while loading, or if it is not possible to determine the value.
function useElasticVersion(datasource: ElasticDatasourceLike): SemVer | null {
  const [version, setVersion] = useState<SemVer | null>(null);
  useEffect(() => {
    let canceled = false;
    datasource.getDatabaseVersion().then(
      (version) => {
        if (!canceled) {
          setVersion(version);
        }
      },
      (error) => {
        // we do nothing
        console.log(error);
      }
    );

    return () => {
      canceled = true;
    };
  }, [datasource]);

  return version;
}

export const QueryEditor = ({ query, onChange, onRunQuery, datasource, range }: ElasticQueryEditorProps) => {
  const elasticVersion = useElasticVersion(datasource);
  const showUnsupportedMessage = elasticVersion != null && !isSupportedVersion(elasticVersion);
  return (
    <ElasticsearchProvider
      datasource={datasource}
      onChange={onChange}
      onRunQuery={onRunQuery}
      query={query}
      range={range || getDefaultTimeRange()}
    >
      {showUnsupportedMessage && <Alert title={unsupportedVersionMessage} />}
      <QueryEditorForm value={query} onChange={onChange} onRunQuery={onRunQuery} />
    </ElasticsearchProvider>
  );
};

const getStyles = (theme: GrafanaTheme2) => ({
  root: css({
    display: 'flex',
  }),
  queryItem: css({
    flexGrow: 1,
    margin: theme.spacing(0, 0.5, 0.5, 0),
  }),
  queryTextArea: css({
    resize: 'none',
    overflow: 'hidden',
    minHeight: theme.spacing(theme.components.height.md),
    fontFamily: theme.typography.fontFamilyMonospace,
  }),
});

interface Props {
  value: ElasticsearchDataQuery;
}

export const ElasticSearchQueryField = ({ value, onChange }: { value?: string; onChange: (v: string) => void }) => {
  const styles = useStyles2(getStyles);
  const textAreaRef = useRef<HTMLTextAreaElement | null>(null);

  const adjustHeight = useCallback(() => {
    const textArea = textAreaRef.current;
    if (!textArea) {
      return;
    }
    textArea.style.height = 'auto';
    // scrollHeight excludes the element's border, but height is set on a
    // border-box element, so add the border back in or the last line gets clipped.
    const borderHeight = textArea.offsetHeight - textArea.clientHeight;
    textArea.style.height = `${textArea.scrollHeight + borderHeight}px`;
  }, []);

  // Grow the textarea to fit its content so long queries word-wrap onto extra
  // lines instead of overflowing/scrolling horizontally like a single-line input.
  useLayoutEffect(() => {
    adjustHeight();
  }, [value, adjustHeight]);

  // Wrap points also change when the available width changes (Explore split-pane
  // drag, panel resize, sidebar toggle). Only react to width changes, not height
  // changes, to avoid a feedback loop with the height updates above.
  useEffect(() => {
    const textArea = textAreaRef.current;
    if (!textArea || typeof ResizeObserver === 'undefined') {
      return;
    }
    let lastWidth: number | null = null;
    const observer = new ResizeObserver(([entry]) => {
      if (entry.contentRect.width !== lastWidth) {
        lastWidth = entry.contentRect.width;
        adjustHeight();
      }
    });
    observer.observe(textArea);
    return () => observer.disconnect();
  }, [adjustHeight]);

  return (
    <div className={styles.queryItem}>
      <TextArea
        ref={textAreaRef}
        className={styles.queryTextArea}
        rows={1}
        value={value ?? ''}
        onChange={(e) => onChange(e.currentTarget.value.replace(/\n/g, ' '))}
        placeholder="Enter a lucene query"
      />
    </div>
  );
};

const QueryEditorForm = ({
  value,
  onChange,
  onRunQuery,
}: Props & { onChange: (query: ElasticsearchDataQuery) => void; onRunQuery: () => void }) => {
  const dispatch = useDispatch();
  const nextId = useNextId();
  const inputId = useId();
  const styles = useStyles2(getStyles);

  const [switchModalOpen, setSwitchModalOpen] = useState(false);
  const [pendingEditorType, setPendingEditorType] = useState<EditorType | null>(null);

  const formatFnRef = useRef<(() => void) | null>(null);
  const onFormatReady = useCallback((fn: () => void) => {
    formatFnRef.current = fn;
  }, []);
  const handleFormat = useCallback(() => {
    formatFnRef.current?.();
  }, []);

  const isTimeSeries = isTimeSeriesQuery(value);

  const isCodeEditor = value.editorType === 'code';

  const queryType: QueryType = value.queryType === 'esql' ? 'esql' : value.queryType === 'dsl' ? 'dsl' : 'lucene';

  // Default to 'builder' if editorType is empty
  const currentEditorType: EditorType = value.editorType === 'code' ? 'code' : 'builder';

  const showBucketAggregationsEditor = value.metrics?.every(
    (metric) => metricAggregationConfig[metric.type].impliedQueryType === 'metrics'
  );

  const isRawDocumentEditor = value.metrics?.every(
    (metric) => metricAggregationConfig[metric.type].impliedQueryType === 'raw_document'
  );

  const onEditorTypeChange = useCallback((newEditorType: EditorType) => {
    // Show warning modal when switching modes
    setPendingEditorType(newEditorType);
    setSwitchModalOpen(true);
  }, []);

  const confirmEditorTypeChange = useCallback(() => {
    if (pendingEditorType) {
      dispatch(
        changeEditorTypeAndResetQuery({
          editorType: pendingEditorType,
          queryType: pendingEditorType === 'builder' ? 'lucene' : 'dsl',
        })
      );
    }
    setSwitchModalOpen(false);
    setPendingEditorType(null);
  }, [dispatch, pendingEditorType]);

  const cancelEditorTypeChange = useCallback(() => {
    setSwitchModalOpen(false);
    setPendingEditorType(null);
  }, []);

  return (
    <>
      <ConfirmModal
        isOpen={switchModalOpen}
        title="Switch editor"
        body="Switching between editors will reset your query. Are you sure you want to continue?"
        confirmText="Continue"
        onConfirm={confirmEditorTypeChange}
        onDismiss={cancelEditorTypeChange}
      />
      <div className={styles.root}>
        <InlineLabel width={17}>Query type</InlineLabel>
        <div className={styles.queryItem}>
          <QueryTypeSelector />
        </div>
        <div style={{ marginLeft: 'auto' }}>
          <EditorTypeSelector value={currentEditorType} onChange={onEditorTypeChange} />
        </div>
      </div>

      {isCodeEditor && (
        <CodeEditorSection
          value={value}
          queryType={queryType}
          showQueryLanguageSelector={true}
          onRunQuery={onRunQuery}
          onFormatReady={onFormatReady}
        />
      )}

      {!isCodeEditor && (
        <>
          <div className={styles.root}>
            <InlineLabel width={17}>Lucene Query</InlineLabel>
            <ElasticSearchQueryField onChange={(query) => dispatch(changeQuery(query))} value={value?.query} />

            {isTimeSeries && (
              <InlineField
                label="Alias"
                labelWidth={15}
                tooltip="Aliasing only works for timeseries queries (when the last group is 'Date Histogram'). For all other query types this field is ignored."
                htmlFor={inputId}
              >
                <Input
                  id={inputId}
                  placeholder="Alias Pattern"
                  onBlur={(e) => dispatch(changeAliasPattern(e.currentTarget.value))}
                  defaultValue={value.alias}
                />
              </InlineField>
            )}
          </div>

          <MetricAggregationsEditor nextId={nextId} />
          {showBucketAggregationsEditor && <BucketAggregationsEditor nextId={nextId} />}
        </>
      )}
      <ElasticsearchQueryOptions
        onFormat={isCodeEditor ? handleFormat : undefined}
        onChange={onChange}
        onRunQuery={onRunQuery}
      />
      {isRawDocumentEditor && <Alert severity="warning" title="The 'Raw Document' query type is deprecated." />}
    </>
  );
};
