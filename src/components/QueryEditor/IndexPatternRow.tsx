import { css } from '@emotion/css';
import React from 'react';

import { GrafanaTheme2 } from '@grafana/data';
import { InlineLabel, useStyles2 } from '@grafana/ui';

import { IndexSelector } from '../IndexSelector';

const getStyles = (theme: GrafanaTheme2) => ({
  root: css({
    display: 'flex',
  }),
  queryItem: css({
    flexGrow: 1,
    margin: theme.spacing(0, 0.5, 0.5, 0),
  }),
});

interface Props {
  value: string | undefined;
  onChange: (index: string | undefined) => void;
}

export const IndexPatternRow = ({ value, onChange }: Props) => {
  const styles = useStyles2(getStyles);

  return (
    <div className={styles.root}>
      <InlineLabel width={17} tooltip="Optionally override the data source index pattern for this query">
        Index
      </InlineLabel>
      <div className={styles.queryItem}>
        <IndexSelector value={value} onChange={onChange} placeholder="Leave empty to use data source index" />
      </div>
    </div>
  );
};
