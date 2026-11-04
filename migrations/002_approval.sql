CREATE SCHEMA IF NOT EXISTS approval;

CREATE TABLE IF NOT EXISTS approval.decisions (
    id UUID PRIMARY KEY,
    -- No cross-schema foreign key: purchase existence is a service concern.
    purchase_id UUID NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
