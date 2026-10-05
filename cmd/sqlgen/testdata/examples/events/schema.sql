-- events: E2E scenario exercising the event system (events.enabled: true).
-- Compact schema — the focus is on event emission across every mutation path.
--
-- Tables:
--   products    — basic CRUD, Upsert (UNIQUE sku), Increment (stock column)
--   articles    — timestamp-based soft delete for SoftDelete/Restore coverage
--   audit_logs  — per-table override disables events (events.enabled: false)
--   accounts    — §32 access-redacted columns (event payload redaction)

CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    sku TEXT NOT NULL UNIQUE,
    price REAL NOT NULL,
    stock INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    body TEXT,
    author TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at DATETIME
);

CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,               -- access: internal  -> redacted from Event.Input
    recovery_code TEXT,                        -- access: write_only -> redacted from Event.Input
    internal_score INTEGER NOT NULL DEFAULT 0, -- access: internal, incrementable -> delta blanked in events
    note TEXT                                  -- public: must survive redaction untouched
);
