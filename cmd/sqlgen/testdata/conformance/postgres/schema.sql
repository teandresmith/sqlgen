-- The conformance schema (docs/design/CONFORMANCE.md §4.1). Every integer is
-- BIGINT and every surrogate key is database-generated, so the generated Go API
-- matches the other dialects' field for field. The postgres_stdlib module reads
-- this file too.

CREATE TABLE products (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL,
    price      BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE order_items (
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL REFERENCES products(id),
    quantity   BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

CREATE TABLE documents (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title      TEXT NOT NULL,
    deleted_at TIMESTAMPTZ,
    -- A soft delete of a pinned document and a restore of an archived one each
    -- violate a CHECK, so the soft-delete family has a constraint to map.
    CONSTRAINT documents_pinned_stays_live CHECK (deleted_at IS NULL OR title <> 'pinned'),
    CONSTRAINT documents_archived_stays_deleted CHECK (deleted_at IS NOT NULL OR title <> 'archived')
);

-- Composite-PK soft delete.
CREATE TABLE revisions (
    document_id BIGINT NOT NULL,
    number      BIGINT NOT NULL,
    body        TEXT NOT NULL,
    deleted_at  TIMESTAMPTZ,
    PRIMARY KEY (document_id, number)
);

-- A case-insensitive key: the database matches a listed key that differs from
-- the stored one only in case.
CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE labels (
    name       CITEXT PRIMARY KEY,
    deleted_at TIMESTAMPTZ
);

-- Increment, the not-null and unique mappings on an insert, and a table with
-- strict_updates off. updated_at has no DEFAULT, so an insert that leaves it out
-- violates NOT NULL.
CREATE TABLE counters (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    hits       BIGINT NOT NULL DEFAULT 0 CHECK (hits >= 0),
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

-- Nested mutations: one parent with an edge of each nestable shape. A UNIQUE
-- name is the upsert conflict target and a constraint a parent write can violate, and
-- deleted_at lets a nested write leave the parent soft-deleted.
CREATE TABLE authors (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    deleted_at TIMESTAMPTZ
);

-- Many-to-many: a UNIQUE tag name is a constraint a nested create can violate.
CREATE TABLE tags (
    id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

-- A pure junction: its key is both foreign keys and it has no other column.
CREATE TABLE author_tags (
    author_id BIGINT NOT NULL REFERENCES authors(id),
    tag_id    BIGINT NOT NULL REFERENCES tags(id),
    PRIMARY KEY (author_id, tag_id)
);

-- Has-one: a UNIQUE nullable foreign key on the child.
CREATE TABLE author_profiles (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id BIGINT UNIQUE REFERENCES authors(id),
    bio       TEXT NOT NULL
);

-- One-to-many on a nullable foreign key, so every verb is generated. The CHECK
-- is a constraint a nested create can violate, and a soft-deleted post is a
-- connect target the visibility read hides.
CREATE TABLE posts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    author_id  BIGINT REFERENCES authors(id),
    title      TEXT NOT NULL CHECK (title <> ''),
    deleted_at TIMESTAMPTZ
);

-- Edges through composite foreign keys. item_notes (O2M) and item_receipts (O2O,
-- a unique member) reach the composite primary key of order_items. parcels (O2M)
-- reach a UNIQUE pair with a nullable referenced column, through nullable
-- members. item_stations links order_items and stations through two composite
-- keys that share product_id, the shape of a tenant junction. stations carries
-- no foreign key of its own, so products keeps its edges.
CREATE TABLE item_notes (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    body       TEXT NOT NULL,
    deleted_at TIMESTAMPTZ,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE item_receipts (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   BIGINT NOT NULL UNIQUE,
    product_id BIGINT NOT NULL,
    code       TEXT NOT NULL,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE shelves (
    id     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region TEXT NOT NULL,
    bin    BIGINT,
    tag    TEXT UNIQUE,
    UNIQUE (region, bin)
);

CREATE TABLE parcels (
    id     BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    region TEXT,
    bin    BIGINT,
    label  TEXT NOT NULL,
    FOREIGN KEY (region, bin) REFERENCES shelves (region, bin)
);

CREATE TABLE stations (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    product_id BIGINT NOT NULL,
    name       TEXT NOT NULL,
    UNIQUE (product_id, id)
);

CREATE TABLE item_stations (
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    station_id BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id, station_id),
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id),
    FOREIGN KEY (product_id, station_id) REFERENCES stations (product_id, id)
);

-- A one-column key into a nullable UNIQUE column, and an M2M from a one-column
-- key into a composite primary key.
CREATE TABLE crates (
    id        BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    shelf_tag TEXT REFERENCES shelves (tag),
    label     TEXT NOT NULL
);

CREATE TABLE crate_items (
    crate_id   BIGINT NOT NULL REFERENCES crates (id),
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    PRIMARY KEY (crate_id, order_id, product_id),
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);
