-- OPTIONAL AI database only. Applied by policy-import, never the MVP runner.
CREATE EXTENSION IF NOT EXISTS vector;
CREATE SCHEMA IF NOT EXISTS ai;
CREATE TABLE IF NOT EXISTS ai.embedding_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL CHECK (dimensions BETWEEN 1 AND 3072)
);
CREATE TABLE IF NOT EXISTS ai.policy_chunks (
    document TEXT NOT NULL CHECK (octet_length(document) BETWEEN 1 AND 120),
    section TEXT NOT NULL CHECK (octet_length(section) BETWEEN 1 AND 180),
    chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
    content TEXT NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 1200),
    content_hash TEXT NOT NULL,
    embedding_model TEXT NOT NULL,
    embedding vector({{DIMENSIONS}}) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (document, section, chunk_index)
);
-- Exact cosine search is adequate for this bounded, at-most-64-chunk corpus.
