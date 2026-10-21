// src/migrations/neo4j/002_create_existence_constraints.cypher
// Creates existence constraints for Neo4j.
// Part of Mnemonic MVP
//
// Dependencies: 001_create_constraints (should be applied first)
//
// IMPORTANT: This migration requires Neo4j Enterprise Edition.
// Existence constraints (IS NOT NULL) are not available in Community Edition.
// Running this file against Community Edition will produce errors.
//
// For local development with Community Edition, skip this migration.
// The application layer enforces these properties regardless of whether
// these constraints are present in Neo4j.
//
// All statements use IF NOT EXISTS for idempotent execution.
//
// Manual application:
//   cypher-shell -u neo4j -p <password> -f src/migrations/neo4j/002_create_existence_constraints.cypher

// =============================================================================
// EXISTENCE CONSTRAINTS (Enterprise Edition only)
// =============================================================================

// Every Pattern node must have a name property.
// The name is synced from PostgreSQL and is required for display and search.
CREATE CONSTRAINT pattern_name_exists IF NOT EXISTS
FOR (p:Pattern) REQUIRE p.name IS NOT NULL;

// Every Agent node must have a name property.
// The name is the primary identifier synced from PostgreSQL.
CREATE CONSTRAINT agent_name_exists IF NOT EXISTS
FOR (a:Agent) REQUIRE a.name IS NOT NULL;

// Every Concept node must have a name property.
// The name is the normalized identifier used for deduplication.
CREATE CONSTRAINT concept_name_exists IF NOT EXISTS
FOR (c:Concept) REQUIRE c.name IS NOT NULL;

// =============================================================================
// SCHEMA VERSION
// =============================================================================

MERGE (v:SchemaVersion {name: 'mnemonic'})
SET v.version = 2, v.migratedAt = datetime(), v.migration = '002_create_existence_constraints';

// =============================================================================
// ROLLBACK
// =============================================================================
// To reverse this migration, run:
//   DROP CONSTRAINT pattern_name_exists IF EXISTS;
//   DROP CONSTRAINT agent_name_exists IF EXISTS;
//   DROP CONSTRAINT concept_name_exists IF EXISTS;
//   MERGE (v:SchemaVersion {name: 'mnemonic'}) SET v.version = 1, v.migratedAt = datetime();
