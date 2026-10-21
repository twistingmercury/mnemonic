package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/twistingmercury/mnemonic/internal/api/config"
	"github.com/twistingmercury/mnemonic/internal/api/version"
	"github.com/twistingmercury/otelx/v2"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Initialize creates and configures the telemetry system using otelx.
// It sets up logging, metrics, and tracing based on the provided configuration.
// otelx installs its providers globally, so instruments come from otel.Meter
// and otel.Tracer. Returns an error if telemetry initialization fails.
func Initialize(ctx context.Context, cfg *config.MnemonicConfig) (*otelx.Telemetry, error) {
	otelCfg, err := buildConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build telemetry config: %w", err)
	}

	tel, err := otelx.Initialize(ctx, otelCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}

	return tel, nil
}

// buildConfig maps the application configuration onto an otelx.Config.
// The log level and tracing endpoint are already checked by config validation.
// Returns an error if the trace exporter cannot be created.
func buildConfig(ctx context.Context, cfg *config.MnemonicConfig) (otelx.Config, error) {
	otelCfg := otelx.Config{
		ServiceName:    version.Service,
		ServiceVersion: version.Version(),
		Environment:    getEnvironment(),
		LogLevel:       cfg.Logging.Level,
	}

	if cfg.Observability.Metrics.Enabled {
		otelCfg.MetricsAddr = fmt.Sprintf(":%d", cfg.Observability.Metrics.Port)
		otelCfg.MetricsPath = cfg.Observability.Metrics.Path
	}

	if cfg.Observability.Tracing.Enabled {
		otelCfg.Tracing = true
		otelCfg.Sampler = sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Observability.Tracing.SampleRate))

		// otelx v2 reads the OTLP endpoint from OTEL_EXPORTER_OTLP_* variables.
		// Build the exporter here so the mnemonic config keeps working.
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Observability.Tracing.Endpoint)}
		if cfg.Observability.Tracing.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return otelx.Config{}, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
		}
		otelCfg.TraceExporter = exporter
	}

	return otelCfg, nil
}

// getEnvironment determines the environment from the MNEMONIC_ENV environment variable.
// Returns "development" if the environment variable is not set.
func getEnvironment() string {
	if env := os.Getenv("MNEMONIC_ENV"); env != "" {
		return env
	}
	return "development"
}
