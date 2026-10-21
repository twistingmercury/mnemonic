// Package middleware provides Gin middleware for request completion logging.
// It writes one bounded, redacted summary per request, correlated with the
// OpenTelemetry span that otelgin starts, with configurable path exclusions.
//
// Documentation:
//   - Architecture: docs/architecture/system/07-observability-architecture.md (MVP > Structured Logging)
//   - Design: docs/design/observability-implementation.md (Structured Logging Implementation)
package middleware
