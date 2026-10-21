package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/twistingmercury/librarian-enricher/internal/config"
	"github.com/twistingmercury/librarian-enricher/internal/version"
	"github.com/twistingmercury/otelx/v2"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Initialize configures logging, metrics and tracing through otelx. When
// metrics are enabled, otelx also sets the global meter provider, so
// otel.Meter records through it; otherwise the global provider is a no-op.
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

// getEnvironment determines the environment from the LIBRARIAN_ENV environment variable.
// Returns "development" if the environment variable is not set.
func getEnvironment() string {
	if env := os.Getenv("LIBRARIAN_ENV"); env != "" {
		return env
	}
	return "development"
}
