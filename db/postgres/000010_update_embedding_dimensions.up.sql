-- src/migrations/postgres/000010_update_embedding_dimensions.up.sql
-- Updates pattern_chunks.embedding from vector(1536) to vector(2000)
-- to support text-embedding-3-large with Matryoshka truncation at 2000 dimensions.
-- 2000 is the maximum supported by pgvector's HNSW and IVFFlat index types.
--
-- Copyright 2025, Mnemonic Authors
--
-- Dependencies:
--   - 000009_pattern_schema_chunks (creates pattern_chunks table with vector(1536))

-- Vector dimensions cannot be altered in place; must drop and recreate the HNSW index.
drop index if exists idx_pattern_chunks_embedding;

-- Update the embedding column to 2000 dimensions.
alter table pattern_chunks
    alter column embedding type vector(2000);

-- Recreate the HNSW index for the new dimension.
create index idx_pattern_chunks_embedding
    on pattern_chunks using hnsw (embedding vector_cosine_ops);

comment on index idx_pattern_chunks_embedding is
    'HNSW index for chunk vector similarity search at 2000 dimensions (text-embedding-3-large with Matryoshka truncation)';
