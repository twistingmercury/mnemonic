-- src/migrations/postgres/000008_create_skill_files.up.sql
-- Creates the skill_files table for files associated with skills.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies: 000007_create_skills (for skills table)
--
-- Skills can have associated files (scripts, references, assets).
-- Each file is identified by the combination of skill_id and path.
--
-- Note: updated_at is managed by the application layer, not database triggers.

create table if not exists skill_files (
    -- UUID primary key
    id uuid primary key default gen_random_uuid(),

    -- Parent skill reference, cascade delete
    skill_id uuid not null references skills(id) on delete cascade,

    -- File path within the skill directory
    path varchar(1024) not null,

    -- File content
    content text not null,

    -- CRC-64 checksum of content for change detection
    crc64 varchar(20) not null,

    -- Audit timestamps
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),

    -- Unique constraint: one file per path per skill
    constraint skill_files_unique_path unique (skill_id, path)
);

-- Index for skill_id lookups (foreign key)
create index idx_skill_files_skill_id on skill_files(skill_id);

comment on table skill_files is 'Child files (scripts, references, assets) for skill definitions';
comment on column skill_files.skill_id is 'Parent skill reference, cascade delete';
comment on column skill_files.path is 'File path within the skill directory';
comment on column skill_files.content is 'File content';
comment on column skill_files.crc64 is 'CRC-64 checksum of content for change detection';
