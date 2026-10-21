// Package pattern provides the PostgreSQL repository implementation for pattern
// persistence. It supports CRUD operations and enrichment status management.
// Semantic search is performed against pattern_chunks embeddings via the chunk
// repository.
//
// Documentation:
//   - Architecture: docs/architecture/04-data-architecture.md (Data Model Design > Patterns)
//   - Design: docs/plans/2026-02-27-pattern-schema-chunks-design.md
package pattern
