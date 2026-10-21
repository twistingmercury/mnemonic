// Package telemetry provides OpenTelemetry initialization for
// Mnemonic's logging, metrics, and distributed tracing. It configures the otelx
// library for application-specific observability with trace-correlated
// structured logging via zerolog.
//
// Documentation:
//   - Architecture: docs/architecture/07-observability-architecture.md (Observability Stage 1: Application Instrumentation (MVP))
//   - Architecture: docs/architecture/07-observability-architecture.md (Observability Stack, Metrics (Prometheus), Logs (Loki), Distributed Tracing (Jaeger))
//   - Design: docs/design/observability-implementation.md (Telemetry Initialization, otelx Package Integration)
package telemetry
