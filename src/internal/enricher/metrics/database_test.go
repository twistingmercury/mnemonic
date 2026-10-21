package metrics_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-enricher/internal/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestDatabaseRecord verifies that every query records its latency, in
// fractional milliseconds, and only a failed query is counted as an error.
func TestDatabaseRecord(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	dm, err := metrics.NewDatabase(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	ctx := context.Background()
	dm.Record(ctx, "neo4j", "SyncPattern", 500*time.Microsecond, nil)
	dm.Record(ctx, "neo4j", "SyncPattern", 2*time.Millisecond, errors.New("connection timeout"))

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &data))

	got := map[string]metricdata.Metrics{}
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = m
		}
	}

	latency := got["mnemonic.db.query_latency"].Data.(metricdata.Histogram[float64])
	require.Len(t, latency.DataPoints, 1)
	assert.Equal(t, uint64(2), latency.DataPoints[0].Count)
	assert.InDelta(t, 2.5, latency.DataPoints[0].Sum, 1e-9, "fractional milliseconds are kept")

	errs := got["mnemonic.db.query_errors"].Data.(metricdata.Sum[int64])
	require.Len(t, errs.DataPoints, 1)
	assert.Equal(t, int64(1), errs.DataPoints[0].Value)
}
