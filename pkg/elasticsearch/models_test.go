package elasticsearch

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuery_IsEsqlQuery(t *testing.T) {
	tests := []struct {
		name      string
		queryType *string
		want      bool
	}{
		{
			name:      "returns false when QueryLanguage is nil",
			queryType: nil,
			want:      false,
		},
		{
			name:      "returns false when QueryType is dsl",
			queryType: strPtr("dsl"),
			want:      false,
		},
		{
			name:      "returns true when QueryType is esql",
			queryType: strPtr("esql"),
			want:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &Query{
				QueryType: tt.queryType,
			}
			if got := q.IsEsqlQuery(); got != tt.want {
				t.Errorf("Query.IsEsqlQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func TestIsSiblingPipelineAgg(t *testing.T) {
	for _, tt := range []struct {
		metricType string
		want       bool
	}{
		{"sum_bucket", true},
		{"max_bucket", true},
		{"min_bucket", true},
		{"avg_bucket", true},
		{"derivative", false},
		{"bucket_script", false},
		{"avg", false},
		{"", false},
	} {
		t.Run(tt.metricType, func(t *testing.T) {
			require.Equal(t, tt.want, isSiblingPipelineAgg(tt.metricType))
		})
	}
}

func TestSiblingAggDisplayNames(t *testing.T) {
	require.Equal(t, "Sum Bucket", metricAggType["sum_bucket"])
	require.Equal(t, "Max Bucket", metricAggType["max_bucket"])
	require.Equal(t, "Min Bucket", metricAggType["min_bucket"])
	require.Equal(t, "Average Bucket", metricAggType["avg_bucket"])
}
