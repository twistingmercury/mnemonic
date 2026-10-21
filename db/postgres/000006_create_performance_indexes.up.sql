-- src/migrations/postgres/000006_create_performance_indexes.up.sql
-- Creates performance-optimized indexes for common query patterns.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000003_create_patterns (for patterns table)
--   - 000005_create_enrichment_jobs (for enrichment_jobs table)

-- =============================================================================
-- PATTERNS INDEXES
-- =============================================================================

-- Partial index for filtering to only enriched patterns
-- Used when selecting patterns eligible for similarity search
create index idx_patterns_enriched
    on patterns(id)
    where enrichment_status = 'enriched';

-- Vector similarity search (IVFFlat for MVP scale)
-- lists = 100 suitable for 1,000-10,000 patterns
-- NOTE: IVFFlat requires at least one row with a non-null embedding to build
-- centroids. If applied to an empty database, create this index after seeding
-- initial data, or use HNSW as an alternative that builds on empty tables.
create index idx_patterns_embedding
    on patterns using ivfflat (embedding vector_cosine_ops)
    with (lists = 100);

-- GIN index for tag filtering using JSONB containment operator (@>)
create index idx_patterns_tags
    on patterns using gin (tags);

-- Full-text search on name and description
create index idx_patterns_search
    on patterns using gin (
        to_tsvector('english', name || ' ' || coalesce(description, ''))
    );

-- =============================================================================
-- ENRICHMENT JOBS INDEXES
-- =============================================================================

-- Pending jobs by scheduled time (worker polling)
create index idx_enrichment_jobs_pending
    on enrichment_jobs(scheduled_for)
    where status = 'pending';

-- Processing jobs for timeout detection
create index idx_enrichment_jobs_processing
    on enrichment_jobs(started_at)
    where status = 'processing';

-- Index documentation
comment on index idx_patterns_embedding is
    'IVFFlat index for vector similarity search (100 lists for MVP scale)';
comment on index idx_enrichment_jobs_pending is
    'Optimizes worker polling for pending jobs';
