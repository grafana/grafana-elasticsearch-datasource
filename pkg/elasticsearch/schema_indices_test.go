package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFieldCapsToColumns(t *testing.T) {
	const sample = `{
  "indices": ["grafana-logs"],
  "fields": {
    "@timestamp": { "date": { "type": "date", "searchable": true } },
    "status": { "keyword": { "type": "keyword" } },
    "msg": { "text": { "type": "text" } }
  }
}`
	cols, err := fieldCapsToColumns([]byte(sample), "@timestamp")
	require.NoError(t, err)
	require.Len(t, cols, 3)

	colsByName := map[string]bool{}
	for _, c := range cols {
		colsByName[c.Name] = true
	}
	require.True(t, colsByName["@timestamp"], "expected @timestamp column")
	require.True(t, colsByName["status"], "expected status column")
	require.True(t, colsByName["msg"], "expected msg column")
}

func TestFieldCapsToColumns_EmptyFields(t *testing.T) {
	const sample = `{"indices": ["grafana-logs"], "fields": {}}`
	_, err := fieldCapsToColumns([]byte(sample), "@timestamp")
	require.ErrorContains(t, err, "no fields returned")
}

func TestFieldCapsToColumns_MultipleIndices(t *testing.T) {
	const sample = `{
  "indices": ["grafana-logs", "grafana-metrics"],
  "fields": {
    "@timestamp": { "date": { "type": "date" } },
    "level": { "keyword": { "type": "keyword" } },
    "cpu": { "float": { "type": "float" } }
  }
}`
	cols, err := fieldCapsToColumns([]byte(sample), "@timestamp")
	require.NoError(t, err)
	require.Len(t, cols, 3)
}
