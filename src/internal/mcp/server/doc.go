// Package server provides HTTP server creation and lifecycle management for
// Mnemonic. It configures the Gin router with middleware, initializes telemetry,
// starts the HTTP listener, and handles graceful shutdown on OS signals.
//
// Documentation:
//   - Architecture: docs/architecture/03-system-architecture.md (Component Breakdown > Mnemonic)
//   - Architecture: docs/architecture/05-deployment-architecture.md (Component Deployment)
//   - Design: docs/design/observability-implementation.md (Server Lifecycle and Telemetry, Initialization and Configuration)
package server
