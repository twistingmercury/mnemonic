-- src/migrations/postgres/000003_create_patterns.up.sql
-- Creates the patterns table with PGVector embedding support.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000001_extensions (for vector extension)
--
-- Patterns use relational columns (not the JSONB document model) because they
-- have enrichment status, graph context, and pgvector embeddings that require
-- individual columns.
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists patterns (
    -- UUID primary key for stable references (patterns may be renamed)
    id uuid primary key default gen_random_uuid(),

    -- Pattern metadata
    name varchar(255) not null,
    description varchar(500),

    -- Pattern content (up to 10KB)
    content text not null,

    -- Categorization tags (JSON array)
    tags jsonb not null default '[]'::jsonb,

    -- Vector embedding for semantic search (1536 dimensions for text-embedding-3-small)
    embedding vector(1536),

    -- Enrichment processing state
    enrichment_status varchar(20) not null default 'pending',
    enrichment_error text,
    enriched_at timestamptz,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Constraints
    constraint patterns_name_unique unique (name),
    constraint patterns_content_length
        check (length(content) <= 10240),
    constraint patterns_tags_array
        check (jsonb_typeof(tags) = 'array'),
    constraint patterns_enrichment_status_valid
        check (enrichment_status in ('pending', 'enriched', 'failed'))
);

comment on table patterns is 'Reusable context patterns for prompt enrichment';
comment on column patterns.embedding is 'Vector embedding (1536d) for semantic similarity search';
comment on column patterns.enrichment_status is 'Processing state: pending, enriched, or failed';
