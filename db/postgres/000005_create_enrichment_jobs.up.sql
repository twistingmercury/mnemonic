-- src/migrations/postgres/000005_create_enrichment_jobs.up.sql
-- Creates the enrichment jobs queue table for background pattern processing.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000003_create_patterns (for patterns table)
--
-- Enrichment jobs track background processing of patterns:
-- - Embedding generation via OpenAI text-embedding-3-small
-- - Entity extraction for Neo4j knowledge graph
-- - Pattern-agent association calculation
--
-- This table serves as a simple Postgres-backed job queue using
-- FOR UPDATE SKIP LOCKED for safe concurrent processing.
--
-- Job lifecycle: pending -> processing -> completed | failed
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists enrichment_jobs (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Reference to pattern being enriched
    pattern_id uuid not null,

    -- Job processing state
    status varchar(20) not null default 'pending',

    -- Retry tracking
    attempts integer not null default 0,
    max_attempts integer not null default 3,

    -- Error information
    last_error text,

    -- Scheduling and timing
    scheduled_for timestamptz not null default now(),
    started_at timestamptz,
    completed_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Foreign key with cascade delete
    constraint fk_enrichment_jobs_pattern
        foreign key (pattern_id) references patterns(id) on delete cascade,

    -- Constraints
    constraint enrichment_jobs_status_valid
        check (status in ('pending', 'processing', 'completed', 'failed')),
    constraint enrichment_jobs_attempts_valid
        check (attempts >= 0),
    constraint enrichment_jobs_max_attempts_valid
        check (max_attempts >= 1)
);

-- Index for pattern lookups
create index idx_enrichment_jobs_pattern
    on enrichment_jobs(pattern_id);

-- Prevent duplicate pending or processing jobs for the same pattern
create unique index idx_enrichment_jobs_unique_pending
    on enrichment_jobs(pattern_id)
    where status in ('pending', 'processing');

comment on table enrichment_jobs is 'Background processing queue for pattern enrichment';
comment on column enrichment_jobs.status is 'Job state: pending, processing, completed, or failed';
comment on column enrichment_jobs.scheduled_for is 'When the job should be processed (supports delayed retry)';
