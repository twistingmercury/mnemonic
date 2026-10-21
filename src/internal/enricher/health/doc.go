// Package health provides health check initialization and execution for
// Mnemonic's database and external service dependencies. It uses the heartbeat
// library to define and evaluate dependency checks for PostgreSQL, Neo4j, and
// OpenAI models.
//
// Documentation:
//   - Architecture: docs/architecture/07-observability-architecture.md (SLOs > Mnemonic SLOs)
package health
