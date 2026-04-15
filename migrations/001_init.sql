-- migrations/001_init.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Users
CREATE TABLE IF NOT EXISTS users (
    id            TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    email         TEXT        NOT NULL UNIQUE,
    name          TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Lists
CREATE TABLE IF NOT EXISTS lists (
    id          TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    name        TEXT        NOT NULL,
    invite_code TEXT        NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- List members (many-to-many users <-> lists)
CREATE TABLE IF NOT EXISTS list_members (
    list_id   TEXT        NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    user_id   TEXT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (list_id, user_id)
);

-- Items
CREATE TABLE IF NOT EXISTS items (
    id         TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    list_id    TEXT        NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    quantity   NUMERIC     NOT NULL DEFAULT 1,
    unit       TEXT        NOT NULL DEFAULT '×',
    checked    BOOLEAN     NOT NULL DEFAULT FALSE,
    checked_by TEXT        REFERENCES users(id),
    checked_at TIMESTAMPTZ,
    added_by   TEXT        NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_items_list_id ON items(list_id);

-- Stores
CREATE TABLE IF NOT EXISTS stores (
    id         TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    list_id    TEXT        NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    trip_count INT         NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(list_id, name)
);

-- Learned aisle order per store (averaged over trips)
CREATE TABLE IF NOT EXISTS store_order_entries (
    store_id  TEXT    NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    item_name TEXT    NOT NULL,
    avg_pos   NUMERIC NOT NULL,
    samples   INT     NOT NULL DEFAULT 1,
    PRIMARY KEY (store_id, item_name)
);

-- Trips (completed shopping sessions)
CREATE TABLE IF NOT EXISTS trips (
    id           TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    list_id      TEXT        NOT NULL REFERENCES lists(id) ON DELETE CASCADE,
    store_id     TEXT        REFERENCES stores(id),
    store_name   TEXT,
    completed_by TEXT        NOT NULL REFERENCES users(id),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    item_count   INT         NOT NULL DEFAULT 0,
    receipt_url  TEXT
);

CREATE INDEX IF NOT EXISTS idx_trips_list_id ON trips(list_id);

-- Items snapshot saved when a trip is completed
CREATE TABLE IF NOT EXISTS trip_items (
    trip_id  TEXT    NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    name     TEXT    NOT NULL,
    quantity NUMERIC NOT NULL,
    unit     TEXT    NOT NULL
);
