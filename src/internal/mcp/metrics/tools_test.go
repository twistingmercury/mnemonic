package metrics_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/mcp/metrics"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestToolsRecord(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	tm, err := metrics.NewTools(provider.Meter("test"))
	require.NoError(t, err)

	ctx := context.Background()
	tm.Record(ctx, "get_pattern", "ok", 500*time.Microsecond)
	tm.Record(ctx, "get_pattern", "ok", time.Millisecond)
	tm.Record(ctx, "get_pattern", "not_found", time.Millisecond)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	got := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = m
		}
	}

	counts := map[string]int64{}
	for _, dp := range got["mnemonic.mcp.tool_calls"].Data.(metricdata.Sum[int64]).DataPoints {
		v, _ := dp.Attributes.Value("outcome")
		counts[v.AsString()] = dp.Value
	}
	assert.Equal(t, map[string]int64{"ok": 2, "not_found": 1}, counts)

	for _, dp := range got["mnemonic.mcp.tool_duration"].Data.(metricdata.Histogram[float64]).DataPoints {
		v, _ := dp.Attributes.Value("outcome")
		if v.AsString() == "ok" {
			assert.InDelta(t, 1.5, dp.Sum, 1e-9, "fractional milliseconds are kept")
		}
	}
}
