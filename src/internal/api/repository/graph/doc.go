// Package graph provides the Neo4j repository the API reads the knowledge
// graph through: related patterns, a pattern's concepts, and cleanup when a
// pattern is deleted. The enricher writes concepts and RELATED_TO edges.
//
// Documentation:
//   - Architecture: docs/architecture/08-data-architecture.md (Neo4j Graph Model, Data Flow Patterns, Data Lifecycle Management)
//   - Architecture: docs/architecture/07-observability-architecture.md (SLOs (Service Level Objectives))
//   - Design: docs/design/data-storage.md (Repository Interfaces > GraphRepository)
package graph
