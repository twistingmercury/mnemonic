package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/otelx/v2"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestRouterTracesAndMeasuresRoutesButNotHealth swaps the global tracer and
// meter providers, so it must not run in parallel.
func TestRouterTracesAndMeasuresRoutesButNotHealth(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	prevTP, prevMP := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetMeterProvider(prevMP)
	})

	gin.SetMode(gin.TestMode)
	router := setupRouter(&otelx.Telemetry{Logger: zerolog.Nop()})
	router.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/version", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, path := range []string{"/health", "/version"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
	}

	spans := recorder.Ended()
	require.Len(t, spans, 1, "only /version is traced")
	assert.Equal(t, "GET /version", spans[0].Name())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	var requests uint64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "http.server.request.duration" {
				for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					requests += dp.Count
				}
			}
		}
	}
	assert.Equal(t, uint64(1), requests, "only /version is measured, through the global meter provider")
}
