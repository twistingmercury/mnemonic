package telemetry_test

import (
	"context"
	"net"
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

// freePort returns a port that is free right now. The telemetry packages run in
// parallel inside one module, so a fixed metrics port collides across them.
// The metrics server binds all interfaces, so probe the same way.
func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func TestInitializeWithMetricsEnabled(t *testing.T) {
	cfg := createTestConfig()
	cfg.Observability.Metrics.Enabled = true
	cfg.Observability.Metrics.Port = freePort(t)

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
