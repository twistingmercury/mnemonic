package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	neo4jcfg "github.com/neo4j/neo4j-go-driver/v5/neo4j/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/twistingmercury/librarian/internal/enricher/metrics"
	"github.com/twistingmercury/librarian/internal/enricher/repository/graph"
)

// Invalid input is rejected before the driver is touched, so a nil driver is fine.
func TestSyncPattern_RejectsInvalidPattern(t *testing.T) {
	repo := graph.NewRepository(nil, "neo4j", nil)
	ctx := context.Background()

	err := repo.SyncPattern(ctx, graph.Pattern{Name: "p"}, nil, 0.3)
	assert.ErrorContains(t, err, "pattern ID must not be nil UUID")

	err = repo.SyncPattern(ctx, graph.Pattern{ID: uuid.New(), Name: "  "}, nil, 0.3)
	assert.ErrorContains(t, err, "pattern name must not be empty")
}

// TestSyncPattern_Instruments verifies that a failed sync produces an errored
// client span and is counted in the database metrics.
func TestSyncPattern_Instruments(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	reader := sdkmetric.NewManualReader()
	dbMetrics, err := metrics.NewDatabase(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	// Nothing listens on port 1, so the transaction fails fast without retries.
	driver, err := neo4j.NewDriverWithContext("bolt://127.0.0.1:1", neo4j.NoAuth(), func(c *neo4jcfg.Config) {
		c.MaxTransactionRetryTime = 0
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	repo := graph.NewRepository(driver, "neo4j", dbMetrics)

	err = repo.SyncPattern(context.Background(), graph.Pattern{ID: uuid.New(), Name: "p"},
		[]graph.Concept{{Name: "go", Type: "technology"}}, 0.3)
	require.Error(t, err)

	var span sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "neo4j SyncPattern" {
			span = s
		}
	}
	require.NotNil(t, span, "expected a neo4j SyncPattern span")
	assert.Equal(t, codes.Error, span.Status().Code)

	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	names := map[string]bool{}
	for _, sm := range data.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	assert.True(t, names["mnemonic.db.query_latency"], "latency should be recorded")
	assert.True(t, names["mnemonic.db.query_errors"], "error should be counted")
}
