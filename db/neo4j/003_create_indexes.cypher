// src/migrations/neo4j/003_create_indexes.cypher
// Creates indexes for common Neo4j query patterns.
// Part of Mnemonic MVP
//
// Dependencies: 001_create_constraints (uniqueness constraints also create indexes,
//   but these additional indexes cover non-unique lookup, relationship properties,
//   and full-text search)
//   Optionally: 002_create_existence_constraints (Enterprise Edition only)
//
// Note: Neo4j automatically creates indexes for uniqueness constraints,
// so Pattern.id, Agent.name, and Concept.name already have indexes from
// migration 001. The indexes below cover additional query patterns.
//
// All indexes use IF NOT EXISTS for idempotent execution.
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/003_create_indexes.cypher

// =============================================================================
// PROPERTY INDEXES
// =============================================================================

// Pattern lookup by name for display and search-by-name queries.
// Note: Pattern.id already has an index from the uniqueness constraint,
// but name lookups are also common when syncing from PostgreSQL.
CREATE INDEX pattern_name_index IF NOT EXISTS
FOR (p:Pattern) ON (p.name);

// Concept filtering by type (technology, practice, domain).
// Used when querying concepts of a specific category for pattern discovery.
CREATE INDEX concept_type_index IF NOT EXISTS
FOR (c:Concept) ON (c.type);

// =============================================================================
// RELATIONSHIP PROPERTY INDEXES
// =============================================================================

// Index on RELEVANT_FOR.relevance for ordering in FindPatternsByAgent queries.
// Without this index, Neo4j sorts by relevance in memory at query time.
// At MVP scale this is acceptable, but the index improves performance as
// the number of RELEVANT_FOR relationships grows.
CREATE INDEX rel_relevant_for_relevance IF NOT EXISTS
FOR ()-[r:RELEVANT_FOR]-() ON (r.relevance);

// =============================================================================
// FULL-TEXT SEARCH INDEXES
// =============================================================================

// Full-text search across pattern name and description fields.
// Enables natural language queries against the knowledge graph.
// Note: Pattern.description may be null; patterns without a description are
// excluded from fulltext search results for description-related queries.
// Usage: CALL db.index.fulltext.queryNodes('pattern_content_fulltext', 'search terms')
CREATE FULLTEXT INDEX pattern_content_fulltext IF NOT EXISTS
FOR (p:Pattern) ON EACH [p.name, p.description];

// Full-text search on concept names.
// Enables fuzzy matching and natural language lookup of concepts.
// Usage: CALL db.index.fulltext.queryNodes('concept_name_fulltext', 'search term')
CREATE FULLTEXT INDEX concept_name_fulltext IF NOT EXISTS
FOR (c:Concept) ON EACH [c.name];

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 3, v.migratedAt = datetime(), v.migration = '003_create_indexes';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP INDEX pattern_name_index IF EXISTS;
//   DROP INDEX concept_type_index IF EXISTS;
//   DROP INDEX pattern_content_fulltext IF EXISTS;
//   DROP INDEX concept_name_fulltext IF EXISTS;
//   DROP INDEX rel_relevant_for_relevance IF EXISTS;
//   -- Community Edition (002 skipped): set version back to 1
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 1, v.migratedAt = datetime();
//   -- Enterprise Edition (002 applied): set version back to 2
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 2, v.migratedAt = datetime();
