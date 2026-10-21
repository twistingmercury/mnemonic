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

	"github.com/twistingmercury/librarian-api/internal/metrics"
	"github.com/twistingmercury/librarian-api/internal/repository/graph"
)

// TestRepository_InstrumentsOperations verifies that a failed operation
// produces an errored client span and is counted in the database metrics.
func TestRepository_InstrumentsOperations(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	reader := sdkmetric.NewManualReader()
	dbMetrics, err := metrics.NewDatabase(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	require.NoError(t, err)

	// Nothing listens on port 1, so the query fails fast without retries.
	driver, err := neo4j.NewDriverWithContext("bolt://127.0.0.1:1", neo4j.NoAuth(), func(c *neo4jcfg.Config) {
		c.MaxTransactionRetryTime = 0
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = driver.Close(context.Background()) })
	repo := graph.NewRepository(driver, "neo4j", dbMetrics)

	require.Error(t, repo.DeletePattern(context.Background(), uuid.New()))

	var span sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "neo4j DeletePattern" {
			span = s
		}
	}
	require.NotNil(t, span, "expected a neo4j DeletePattern span")
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
