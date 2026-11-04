CREATE SCHEMA IF NOT EXISTS purchase;

CREATE TABLE IF NOT EXISTS purchase.requests (
    id UUID PRIMARY KEY,
    client_request_id TEXT NOT NULL UNIQUE
        CHECK (client_request_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'),
    employee_id TEXT NOT NULL
        CHECK (employee_id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'),
    item_name TEXT NOT NULL CHECK (char_length(item_name) BETWEEN 1 AND 200 AND item_name ~ '[^[:space:]]'),
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0),
    total_cents BIGINT NOT NULL CHECK (total_cents >= 0),
    -- NUMERIC is used only for exact constraint evaluation to avoid BIGINT overflow.
    CONSTRAINT requests_exact_total CHECK (total_cents::numeric = quantity::numeric * unit_price_cents::numeric),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
