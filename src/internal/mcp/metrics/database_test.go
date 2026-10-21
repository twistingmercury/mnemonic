package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/mcp/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestDatabaseRecord verifies that every operation records its latency and
// only a failed one is counted as an error.
func TestDatabaseRecord(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	dm, err := metrics.NewDatabase(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	ctx := context.Background()
	dm.Record(ctx, "neo4j", "select", 50*time.Millisecond, nil)
	dm.Record(ctx, "neo4j", "select", 5000*time.Millisecond, errors.New("connection timeout"))

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))

	var latencyCount uint64
	var errorCount int64
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "mnemonic.db.query_latency":
				for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					latencyCount += dp.Count
				}
			case "mnemonic.db.query_errors":
				for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
					errorCount += dp.Value
				}
			}
		}
	}
	assert.Equal(t, uint64(2), latencyCount, "both operations should record latency")
	assert.Equal(t, int64(1), errorCount, "only the failed operation should be counted")
}

// TestDatabaseRecord_SubMillisecond verifies that sub-millisecond latencies
// are recorded as fractions rather than truncated to zero.
func TestDatabaseRecord_SubMillisecond(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	dm, err := metrics.NewDatabase(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	ctx := context.Background()
	dm.Record(ctx, "neo4j", "select", 500*time.Microsecond, nil)

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.db.query_latency" {
				hist := m.Data.(metricdata.Histogram[float64])
				require.Len(t, hist.DataPoints, 1)
				assert.InDelta(t, 0.5, hist.DataPoints[0].Sum, 1e-9)
				return
			}
		}
	}
	t.Fatal("query latency metric not recorded")
}
