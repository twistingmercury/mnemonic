-- src/migrations/postgres/000004_create_pattern_agent_associations.up.sql
-- Creates the pattern-agent association table for many-to-many relationships.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000002_create_agents (for agents table)
--   - 000003_create_patterns (for patterns table)
--
-- This table establishes which patterns are relevant to which agents,
-- with a relevance score indicating how strongly they relate.

create table if not exists pattern_agent_associations (
    -- Composite primary key
    pattern_id uuid not null,
    agent_id uuid not null,

    -- Relevance score (0.0 to 1.0)
    relevance double precision not null,

    -- Foreign keys
    constraint fk_pattern_agent_assoc_pattern
        foreign key (pattern_id) references patterns(id) on delete cascade,
    constraint fk_pattern_agent_assoc_agent
        foreign key (agent_id) references agents(id) on delete cascade,

    -- Primary key
    primary key (pattern_id, agent_id),

    -- Constraints
    constraint pattern_agent_assoc_relevance_range
        check (relevance >= 0 and relevance <= 1)
);

-- Index for reverse FK lookup (agent_id is not the leading PK column).
-- pattern_id lookup is covered by the composite PK index.
create index idx_pattern_agent_assoc_agent
    on pattern_agent_associations(agent_id);

comment on table pattern_agent_associations is
    'Many-to-many relationship between patterns and agents with relevance scores';
