-- tenancy: E2E scenario exercising sqlgen's tenancy primitive (PRD §29).
--
-- Schema covers every shape the generator must handle for tenanted projects:
--   workspaces       — tenant identity table itself (no workspace_id column)
--   products         — tenanted, simple PK (id), workspace_id auto-filtered
--   order_items      — composite PK (workspace_id, order_id, product_id):
--                      tenant column IS part of the PK (PRD §29.7)
--   audit_logs       — shared / opted-out (no workspace_id column at all)
--   legacy_widgets   — uses org_id instead of workspace_id (per-table column override)
--   users            — tenanted; o2o → user_profiles (tenanted child)
--   user_profiles    — tenanted o2o child of users
--   posts            — tenanted o2m child of users (posts.user_id FK)
--   tags             — tenanted m2m peer of posts via post_tags junction
--   post_tags        — non-tenanted junction between posts and tags
--   tag_links        — TENANTED pure junction: columns are exactly the
--                      composite key plus workspace_id, so an upsert on the PK
--                      has nothing to SET (the DO NOTHING branch)
--   articles         — tenanted with timestamp soft delete (deleted_at)
--                      exercises §29.8 soft-delete + tenancy composition, and
--                      as a nullable-FK o2m child of users it is the
--                      relationship-filter target that carries both rules

-- workspace_id columns are BLOB (16 bytes) so uuid.UUID round-trips naturally
-- under modernc.org/sqlite. SQLite has no native UUID type — BLOB is the
-- conventional storage shape for [16]byte UUIDs.

CREATE TABLE workspaces (
    id BLOB PRIMARY KEY,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id BLOB NOT NULL,
    name TEXT NOT NULL,
    sku TEXT NOT NULL,
    price REAL NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE (workspace_id, sku)
);

-- order_items: composite PK with tenant column IN the PK (§29.7). The caller
-- supplies workspace_id as part of the OrderItemPK struct on every PK-based
-- operation; the runtime verifies the supplied value matches the resolved
-- tenant and short-circuits with tenancy.ErrMismatch before any DB round-trip
-- when they diverge.
CREATE TABLE order_items (
    workspace_id BLOB NOT NULL,
    order_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price REAL NOT NULL,
    PRIMARY KEY (workspace_id, order_id, product_id)
);

CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE legacy_widgets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    org_id BLOB NOT NULL,
    label TEXT NOT NULL
);

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id BLOB NOT NULL,
    email TEXT NOT NULL,
    UNIQUE (workspace_id, email)
);

CREATE TABLE user_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    workspace_id BLOB NOT NULL,
    bio TEXT,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE posts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    workspace_id BLOB NOT NULL,
    title TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id BLOB NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (workspace_id, name)
);

CREATE TABLE post_tags (
    post_id INTEGER NOT NULL,
    tag_id INTEGER NOT NULL,
    PRIMARY KEY (post_id, tag_id),
    FOREIGN KEY (post_id) REFERENCES posts(id),
    FOREIGN KEY (tag_id) REFERENCES tags(id)
);

-- tag_links: a tenanted pure junction — its columns are exactly its composite
-- key plus the tenant column. Upserting it on the PK leaves nothing to SET, so
-- the dialect emits DO NOTHING and the statement returns no row.
-- Unlike order_items, workspace_id is deliberately NOT part of the PK, so the
-- tenant-capture branch (PRD §29.5) is the one under test: the PK is
-- caller-known, but the tenant still has to come back from the row.
CREATE TABLE tag_links (
    tag_id INTEGER NOT NULL,
    related_tag_id INTEGER NOT NULL,
    workspace_id BLOB NOT NULL,
    PRIMARY KEY (tag_id, related_tag_id),
    FOREIGN KEY (tag_id) REFERENCES tags(id),
    FOREIGN KEY (related_tag_id) REFERENCES tags(id)
);

-- articles.user_id is nullable so pre-existing article fixtures keep working,
-- and it makes articles an o2m target that is BOTH tenanted and soft-deleted —
-- the pairing a relationship filter's subquery has to scope on two rules at
-- once (PRD §11.1 invariant 1, §17.3).
CREATE TABLE articles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id BLOB NOT NULL,
    user_id INTEGER,
    title TEXT NOT NULL,
    body TEXT,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    deleted_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
