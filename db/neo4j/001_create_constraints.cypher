// src/migrations/neo4j/001_create_constraints.cypher
// Creates uniqueness constraints for Neo4j node labels.
// Part of Mnemonic MVP
//
// Dependencies: None (first Neo4j migration)
//
// This migration establishes uniqueness constraints for the three
// node types in the knowledge graph: Pattern, Agent, and Concept.
// All constraints use IF NOT EXISTS for idempotent execution.
//
// These constraints are compatible with Neo4j Community Edition.
// For existence constraints (Enterprise Edition only), see
// 002_create_existence_constraints.cypher.
//
// Neo4j is a best-effort graph projection of PostgreSQL data.
// Mnemonic validates these constraints at startup:
//   - All constraints exist  -> log info, continue
//   - Some missing           -> log warning with names, continue
//   - Connection failure     -> log warning, continue
//
// Constraint names checked at startup:
//   pattern_id_unique, agent_name_unique, concept_name_unique
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/001_create_constraints.cypher

// =============================================================================
// UNIQUENESS CONSTRAINTS
// =============================================================================

// Pattern nodes use the UUID from PostgreSQL as their unique identifier.
// This ensures one-to-one correspondence between Postgres rows and graph nodes.
CREATE CONSTRAINT pattern_id_unique IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.id IS UNIQUE;

// Agent nodes use the agent name from PostgreSQL as their unique identifier.
// The name serves as both the natural key in Postgres and the graph node key.
CREATE CONSTRAINT agent_name_unique IF NOT EXISTS
FOR (a:Agent) REQUIRE a.name IS UNIQUE;

// Concept nodes use a normalized lowercase name as their unique identifier.
// Concepts are extracted during enrichment and deduplicated by name.
CREATE CONSTRAINT concept_name_unique IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS UNIQUE;

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 1, v.migratedAt = datetime(), v.migration = '001_create_constraints';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP CONSTRAINT pattern_id_unique IF EXISTS;
//   DROP CONSTRAINT agent_name_unique IF EXISTS;
//   DROP CONSTRAINT concept_name_unique IF EXISTS;
//   MATCH (v:SchemaVersion {name: 'mnemonic'}) DELETE v;
