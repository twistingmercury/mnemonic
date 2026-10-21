// Package graph provides the read-only Neo4j repository for the knowledge
// graph: concepts linked to patterns via MENTIONED_IN, and pattern similarity
// via pre-computed RELATED_TO edges. Each query gets a client span and, when
// metrics are enabled, a latency and error recording.
package graph
