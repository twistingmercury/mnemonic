package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/api/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestPatternRecordQuery(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	pm, err := metrics.NewPattern(meter)
	require.NoError(t, err)

	ctx := context.Background()
	pm.RecordQuery(ctx, "postgres", 50*time.Millisecond, 10)
	pm.RecordQuery(ctx, "pgvector", 100*time.Millisecond, 5)
	pm.RecordQuery(ctx, "neo4j", 75*time.Millisecond, 8)

	var data metricdata.ResourceMetrics
	err = reader.Collect(ctx, &data)
	require.NoError(t, err)

	assert.NotEmpty(t, data.ScopeMetrics)
	foundQueryLatency := false
	foundPatternsReturned := false
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "mnemonic.patterns.query_latency" {
				foundQueryLatency = true
			}
			if m.Name == "mnemonic.patterns.returned" {
				foundPatternsReturned = true
			}
		}
	}
	assert.True(t, foundQueryLatency, "query latency metric should be recorded")
	assert.True(t, foundPatternsReturned, "patterns returned metric should be recorded")
}
