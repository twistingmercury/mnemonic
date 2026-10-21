-- src/migrations/postgres/000002_create_agents.up.sql
-- Creates the agents table with JSONB document model.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000001_extensions
--
-- Agents use the JSONB document model: only the fields required for
-- database-level operations (lookup key, change detection, audit timestamps)
-- are top-level columns. Everything else lives inside the JSONB definition.
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists agents (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Unique lookup key: lowercase-with-hyphens format, URL-safe
    name varchar(255) unique not null,

    -- Complete agent definition as JSONB document
    definition jsonb not null,

    -- CRC-64 checksum of serialized definition for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- GIN index on definition for JSONB queries
create index idx_agents_definition on agents using gin (definition);

comment on table agents is 'Agent definitions stored as JSONB documents for team tooling synchronization';
comment on column agents.id is 'UUID primary key';
comment on column agents.name is 'Unique lookup key, lowercase-with-hyphens format';
comment on column agents.definition is 'Complete agent definition as JSONB document';
comment on column agents.crc64 is 'CRC-64 checksum of serialized definition for change detection';
