package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/enricher/config"
	"github.com/twistingmercury/mnemonic/internal/enricher/telemetry"
)

// createTestConfig sets the sections telemetry reads, with metrics and
// tracing disabled.
func createTestConfig() *config.MnemonicConfig {
	return &config.MnemonicConfig{
		Logging: config.LoggingConfig{Level: "info"},
		Observability: config.ObservabilityConfig{
			Metrics: config.MetricsConfig{Path: "/metrics", Port: 9090},
			Tracing: config.TracingConfig{SampleRate: 0.1, OTLPInsecure: true},
		},
	}
}

func TestInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, createTestConfig())
	require.NoError(t, err)
	assert.NoError(t, tel.Shutdown(ctx))
}

func TestInitializeWithMetricsEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Metrics.Enabled = true
	cfg.Observability.Metrics.Port = 19090 // Use a high port to avoid conflicts

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	assert.NoError(t, tel.Shutdown(ctx))
}

func TestInitializeWithTracingEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Tracing.Enabled = true
	cfg.Observability.Tracing.Endpoint = "localhost:4317"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tel, err := telemetry.Initialize(ctx, cfg)
	require.NoError(t, err)
	defer func() {
		_ = tel.Shutdown(ctx)
	}()

	assert.NotNil(t, tel.TracerProvider)
}
