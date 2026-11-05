-- The gateway's identity module owns auth.*. Procurement services never query it.
CREATE SCHEMA IF NOT EXISTS auth;
CREATE TABLE auth.users (
    -- Keep legacy employee IDs stable so existing requests retain their owners.
    id TEXT PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$'),
    username TEXT NOT NULL UNIQUE CHECK (username ~ '^[a-z0-9][a-z0-9._-]{2,79}$'),
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    password_hash TEXT NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    role TEXT NOT NULL DEFAULT 'employee' CHECK (role IN ('employee', 'manager', 'admin')),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE auth.role_changes (
    id UUID PRIMARY KEY,
    actor_id TEXT NOT NULL REFERENCES auth.users(id),
    user_id TEXT NOT NULL REFERENCES auth.users(id),
    old_role TEXT NOT NULL CHECK (old_role IN ('employee', 'manager', 'admin')),
    new_role TEXT NOT NULL CHECK (new_role IN ('employee', 'manager', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
