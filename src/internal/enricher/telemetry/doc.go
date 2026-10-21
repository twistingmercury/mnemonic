// Package telemetry provides OpenTelemetry initialization and access for
// Mnemonic's logging, metrics, and distributed tracing. It wraps the otelx
// library to configure application-specific observability with trace-correlated
// structured logging via zerolog.
//
// Documentation:
//   - Architecture: docs/architecture/07-observability-architecture.md (Observability Stage 1: Application Instrumentation (MVP))
//   - Architecture: docs/architecture/07-observability-architecture.md (Observability Stack, Metrics (Prometheus), Logs (Loki), Distributed Tracing (Jaeger))
//   - Design: docs/design/observability-implementation.md (Telemetry Initialization, otelx Package Integration)
package telemetry
