// Package integration tests the whole Mnemonic stack through its public HTTP
// interfaces: the REST API, the MCP endpoint, each service's /health, and the
// OpenAI stub's control endpoint. It covers only what no single service's own
// E2E suite can: a pattern written through the API, enriched by the enricher
// via RabbitMQ, and read back through the MCP server.
//
// It needs the Compose stack in the repository root; run it with make test.
package integration
