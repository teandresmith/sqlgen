-- The database's current schema. sqlgen reads it by introspection
-- (input.source: both); migrations/ holds the changes not yet applied.

CREATE TABLE accounts (
    id           INTEGER PRIMARY KEY,
    email        TEXT NOT NULL,
    display_name TEXT NOT NULL,
    legacy_code  TEXT,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts (id),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    archived   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX projects_active_name_idx ON projects (name) WHERE archived = 0;

CREATE TABLE project_members (
    project_id INTEGER NOT NULL REFERENCES projects (id),
    account_id INTEGER NOT NULL REFERENCES accounts (id),
    role       TEXT NOT NULL,
    PRIMARY KEY (account_id, project_id)
);

CREATE TABLE tasks (
    id               INTEGER PRIMARY KEY,
    project_id       INTEGER NOT NULL REFERENCES projects (id),
    title            TEXT NOT NULL,
    estimate_minutes INTEGER NOT NULL DEFAULT 0,
    estimate_seconds INTEGER GENERATED ALWAYS AS (estimate_minutes * 60) STORED
);

CREATE TABLE legacy_events (
    id      INTEGER PRIMARY KEY,
    payload TEXT NOT NULL
);
