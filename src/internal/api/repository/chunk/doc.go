// Package chunk provides the PostgreSQL repository implementation for pattern
// chunk persistence. Each chunk is one H2-bounded section of a parent pattern,
// with its own vector embedding for semantic similarity search via pgvector.
//
// Documentation:
//   - Architecture: docs/architecture/04-data-architecture.md (Data Model Design > pattern_chunks)
//   - Design: docs/plans/2026-02-27-pattern-schema-chunks-design.md (Chunk Repository Package)
package chunk
