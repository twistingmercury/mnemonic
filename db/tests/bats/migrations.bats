#!/usr/bin/env bats
# BATS tests for PostgreSQL migration verification
#
# Up-migration tests verify the final state after all migrations have run.
#
# Connection details are taken from environment variables that the test runner
# sets, or the defaults exported below.

# ---------------------------------------------------------------------------
# Connection defaults (overridable by environment)
# ---------------------------------------------------------------------------
export PGHOST="${PGHOST:-localhost}"
export PGPORT="${PGPORT:-5435}"
export PGDATABASE="${PGDATABASE:-mnemonic}"
export PGUSER="${PGUSER:-mnemonic}"
export PGPASSWORD="${PGPASSWORD:-mnemonic_dev}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# run_psql: execute a query, print trimmed output to stdout
run_psql() {
    local query="$1"
    psql -t -A -c "$query" 2>&1
}

# repo_root: locate repository root relative to this test file
repo_root() {
    cd "$(dirname "${BATS_TEST_FILENAME}")/../../../.." && pwd
}

# ---------------------------------------------------------------------------
# UP MIGRATION TESTS
# These tests assume all migrations have already been applied by the test
# runner (via golang-migrate "up") before BATS starts.
# ---------------------------------------------------------------------------

# --- Extensions (000001) ---

@test "up: extension vector exists" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_extension WHERE extname = 'vector';")
    [ -n "$result" ]
}

# Note: 000001 only installs `vector`; uuid-ossp is not installed.

# --- Tables ---

@test "up: table agents exists (000002)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'agents';")
    [ -n "$result" ]
}

@test "up: table patterns exists (000003)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'patterns';")
    [ -n "$result" ]
}

@test "up: table pattern_agent_associations exists (000004)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'pattern_agent_associations';")
    [ -n "$result" ]
}

@test "up: table enrichment_jobs exists (000005)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'enrichment_jobs';")
    [ -n "$result" ]
}

@test "up: table skills exists (000007)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'skills';")
    [ -n "$result" ]
}

@test "up: table skill_files exists (000008)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'skill_files';")
    [ -n "$result" ]
}

@test "up: table pattern_chunks exists (000009)" {
    local result
    result=$(run_psql "SELECT 1 FROM pg_tables WHERE schemaname = 'public' AND tablename = 'pattern_chunks';")
    [ -n "$result" ]
}

# --- Schema spot-checks ---

@test "up: pattern_chunks.embedding is vector(2000) (000010)" {
    local col_type
    col_type=$(run_psql "
        SELECT format_type(a.atttypid, a.atttypmod)
        FROM pg_attribute a
        JOIN pg_class c ON c.oid = a.attrelid
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relname  = 'pattern_chunks'
          AND a.attname  = 'embedding';
    ")
    [ "$col_type" = "vector(2000)" ]
}

@test "up: enrichment_jobs has pattern_id column (000005)" {
    local result
    result=$(run_psql "
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name   = 'enrichment_jobs'
          AND column_name  = 'pattern_id';
    ")
    [ -n "$result" ]
}

@test "up: enrichment_jobs has chunk_id column (000009)" {
    local result
    result=$(run_psql "
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name   = 'enrichment_jobs'
          AND column_name  = 'chunk_id';
    ")
    [ -n "$result" ]
}

# pattern_id became nullable in 000009
@test "up: enrichment_jobs.pattern_id is nullable (000009)" {
    local result
    result=$(run_psql "
        SELECT is_nullable
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name   = 'enrichment_jobs'
          AND column_name  = 'pattern_id';
    ")
    [ "$result" = "YES" ]
}

# --- Indexes ---

@test "up: idx_pattern_chunks_embedding HNSW index exists (000009/000010)" {
    local result
    result=$(run_psql "
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND indexname   = 'idx_pattern_chunks_embedding';
    ")
    [ -n "$result" ]
}

@test "up: at least one index on enrichment_jobs exists (000005)" {
    local count
    count=$(run_psql "
        SELECT COUNT(*)
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND tablename  = 'enrichment_jobs'
          AND indexname LIKE 'idx_%';
    ")
    [ "$count" -ge 1 ]
}

@test "up: idx_patterns_enriched exists (000006)" {
    local result
    result=$(run_psql "
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND indexname   = 'idx_patterns_enriched';
    ")
    [ -n "$result" ]
}

@test "up: idx_agents_definition GIN index exists (000002)" {
    local result
    result=$(run_psql "
        SELECT 1
        FROM pg_indexes
        WHERE schemaname = 'public'
          AND indexname   = 'idx_agents_definition';
    ")
    [ -n "$result" ]
}
