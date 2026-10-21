package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/enricher/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestJobsRecord(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	jm, err := metrics.NewJobs(provider.Meter("test"))
	require.NoError(t, err)

	ctx := context.Background()
	jm.Record(ctx, metrics.OutcomeCompleted, 1500*time.Microsecond)
	jm.Record(ctx, metrics.OutcomeCompleted, 2*time.Millisecond)
	jm.Record(ctx, metrics.OutcomeFailed, time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	got := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = m
		}
	}

	counts := map[string]int64{}
	for _, dp := range got["mnemonic.enrichment.jobs"].Data.(metricdata.Sum[int64]).DataPoints {
		v, _ := dp.Attributes.Value("outcome")
		counts[v.AsString()] = dp.Value
	}
	assert.Equal(t, map[string]int64{"completed": 2, "failed": 1}, counts)

	for _, dp := range got["mnemonic.enrichment.job_duration"].Data.(metricdata.Histogram[float64]).DataPoints {
		v, _ := dp.Attributes.Value("outcome")
		if v.AsString() == "completed" {
			assert.InDelta(t, 3.5, dp.Sum, 1e-9, "fractional milliseconds are kept")
		}
	}
}
