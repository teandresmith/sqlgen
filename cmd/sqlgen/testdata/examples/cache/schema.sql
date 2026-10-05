-- cache: E2E scenario exercising the cache system (cache.enabled: true).
-- Compact schema — the focus is on cache read-through, invalidation, hydration,
-- composite-PK round-trip, relationship bypass, and view invalidation.
--
-- Tables:
--   products         — single-PK int64; Upsert via UNIQUE sku, Increment on stock
--   articles         — single-PK int64; timestamp soft delete for SoftDelete/Restore coverage
--   audit_logs       — per-table cache override disables caching (opt-out)
--   order_items      — composite PK (order_id, product_id) for composite round-trip test
--   users            — single-PK int64 with an O2O relationship to profiles (bypass test)
--   profiles         — O2O target for users
--
-- View:
--   product_summary  — aggregates products; cache enabled, invalidate_on: [products]

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

CREATE TABLE order_items (
    order_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE
);

CREATE TABLE profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    bio TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
