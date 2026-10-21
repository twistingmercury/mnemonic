-- src/migrations/postgres/000001_extensions.up.sql
-- Enables required PostgreSQL extensions.
-- Part of Mnemonic MVP
--
-- Copyright 2025, Mnemonic Authors
--
-- This migration must be applied first as it provides:
-- - Vector operations extension for pattern embeddings

-- Enable vector operations for embeddings (pgvector extension)
create extension if not exists vector;
