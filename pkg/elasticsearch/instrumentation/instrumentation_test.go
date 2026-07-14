package instrumentation

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gaugeValue(t *testing.T, gauge prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, gauge.Write(&m))
	return m.GetGauge().GetValue()
}

func TestDatasourceInstances(t *testing.T) {
	t.Run("Should track active instances per distribution and major version", func(t *testing.T) {
		gauge := DatasourceInstances.WithLabelValues("elasticsearch", "9")

		before := gaugeValue(t, gauge)

		DatasourceInstances.WithLabelValues("elasticsearch", "9").Inc()
		assert.Equal(t, before+1, gaugeValue(t, gauge))

		DatasourceInstances.WithLabelValues("elasticsearch", "9").Dec()
		assert.Equal(t, before, gaugeValue(t, gauge))
	})

	t.Run("Should track distributions independently", func(t *testing.T) {
		other := DatasourceInstances.WithLabelValues("customdistro", "2")
		before := gaugeValue(t, other)

		DatasourceInstances.WithLabelValues("elasticsearch", "8").Inc()

		assert.Equal(t, before, gaugeValue(t, other))
	})
}
