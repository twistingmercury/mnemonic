#!/usr/bin/env bats
# BATS tests for Neo4j migration verification

# Connection details (exported by test runner)
# NEO4J_CONTAINER — Docker container ID
# NEO4J_USER — neo4j
# NEO4J_PASSWORD — mnemonic_dev

# Helper: Run cypher-shell query and capture output
run_cypher() {
    local query="$1"
    docker exec "${NEO4J_CONTAINER}" cypher-shell \
        -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" \
        --format plain "$query" 2>&1
}

# =============================================================================
# Up Migration Verification Tests
# Migrations have already been applied by the test runner.
# =============================================================================

@test "constraint pattern_id_unique exists" {
    local query="SHOW CONSTRAINTS WHERE name = 'pattern_id_unique'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "constraint agent_name_unique exists" {
    local query="SHOW CONSTRAINTS WHERE name = 'agent_name_unique'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "constraint concept_name_unique exists" {
    local query="SHOW CONSTRAINTS WHERE name = 'concept_name_unique'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "index pattern_name_index exists" {
    local query="SHOW INDEXES WHERE name = 'pattern_name_index'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "index concept_type_index exists" {
    local query="SHOW INDEXES WHERE name = 'concept_type_index'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "index pattern_content_fulltext exists" {
    local query="SHOW INDEXES WHERE name = 'pattern_content_fulltext'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "index concept_name_fulltext exists" {
    local query="SHOW INDEXES WHERE name = 'concept_name_fulltext'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "index rel_relevant_for_relevance exists" {
    local query="SHOW INDEXES WHERE name = 'rel_relevant_for_relevance'"
    local result
    result=$(run_cypher "$query")

    [ -n "$result" ]
}

@test "expected number of constraints exist" {
    # Expected: 3 uniqueness constraints (pattern_id_unique, agent_name_unique, concept_name_unique)
    # Skipping Enterprise Edition existence constraints (not available in Community Edition)
    local query="SHOW CONSTRAINTS YIELD name RETURN count(*) AS count"
    local result
    result=$(run_cypher "$query")
    local count
    count=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)

    [ "$count" -eq 3 ]
}

@test "expected number of indexes exist" {
    # Expected: at least 8 indexes
    #   - 3 auto-created by uniqueness constraints (pattern_id, agent_name, concept_name)
    #   - 5 explicit indexes from 003_create_indexes.cypher
    local query="SHOW INDEXES YIELD name RETURN count(*) AS count"
    local result
    result=$(run_cypher "$query")
    local count
    count=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)

    [ "$count" -ge 8 ]
}

@test "schema version node exists with correct version" {
    # Expected: SchemaVersion node with name='mnemonic' and version=3
    # (Migration 001 set version=1, migration 003 set version=3, migration 002 skipped)
    local query="MATCH (v:SchemaVersion {name: 'mnemonic'}) RETURN v.version AS version"
    local result
    result=$(run_cypher "$query")
    local version
    version=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)

    [ "$version" -eq 3 ]
}

@test "migrations are idempotent on re-application" {
    # Re-apply migration 001 (already in container from initial run)
    docker exec "${NEO4J_CONTAINER}" cypher-shell \
        -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" \
        -f /tmp/001_create_constraints.cypher >/dev/null 2>&1

    # Re-apply migration 003 (already in container from initial run)
    docker exec "${NEO4J_CONTAINER}" cypher-shell \
        -u "${NEO4J_USER}" -p "${NEO4J_PASSWORD}" \
        -f /tmp/003_create_indexes.cypher >/dev/null 2>&1

    # Verify constraint count unchanged (still 3)
    local query="SHOW CONSTRAINTS YIELD name RETURN count(*) AS count"
    local result
    result=$(run_cypher "$query")
    local count
    count=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)
    [ "$count" -eq 3 ]

    # Verify index count unchanged (still >= 8)
    query="SHOW INDEXES YIELD name RETURN count(*) AS count"
    result=$(run_cypher "$query")
    count=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)
    [ "$count" -ge 8 ]

    # Verify SchemaVersion still correct
    query="MATCH (v:SchemaVersion {name: 'mnemonic'}) RETURN v.version AS version"
    result=$(run_cypher "$query")
    local version
    version=$(printf '%s' "$result" | grep -E '^[0-9]+$' | head -n 1)
    [ "$version" -eq 3 ]
}
