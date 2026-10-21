-- src/migrations/postgres/000007_create_skills.up.sql
-- Creates the skills table with JSONB document model.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Skills use the same JSONB document model as agents: only lookup key,
-- change detection, and audit timestamps are top-level columns.
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists skills (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Unique lookup key: matches Claude Code skill directory name
    name varchar(255) unique not null,

    -- Complete skill definition as JSONB document
    definition jsonb not null,

    -- CRC-64 checksum of serialized definition for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- GIN index on definition for tag filtering and JSONB queries
create index idx_skills_definition on skills using gin (definition);

comment on table skills is 'Skill definitions stored as JSONB documents for team tooling synchronization';
comment on column skills.name is 'Unique lookup key, lowercase-with-hyphens (matches Claude Code skill directory name)';
comment on column skills.definition is 'Complete skill definition as JSONB document (Agent Skills spec aligned)';
comment on column skills.crc64 is 'CRC-64 checksum of serialized definition for change detection';
