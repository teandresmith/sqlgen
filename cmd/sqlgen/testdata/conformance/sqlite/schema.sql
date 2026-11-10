-- The conformance schema (docs/design/CONFORMANCE.md §4.1). Every integer is
-- BIGINT and every surrogate key is database-generated, so the generated Go API
-- matches the other dialects' field for field. A surrogate key is INTEGER
-- PRIMARY KEY because only that spelling is SQLite's rowid alias.

CREATE TABLE products (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    price      BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE order_items (
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL REFERENCES products(id),
    quantity   BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

CREATE TABLE documents (
    id         INTEGER PRIMARY KEY,
    title      TEXT NOT NULL,
    deleted_at TIMESTAMP,
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
    deleted_at  TIMESTAMP,
    PRIMARY KEY (document_id, number)
);

-- A case-insensitive key: the database matches a listed key that differs from
-- the stored one only in case.
CREATE TABLE labels (
    name       TEXT COLLATE NOCASE NOT NULL PRIMARY KEY,
    deleted_at TIMESTAMP
);

-- Increment, the not-null and unique mappings on an insert, and a table with
-- strict_updates off. updated_at has no DEFAULT, so an insert that leaves it out
-- violates NOT NULL.
CREATE TABLE counters (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    hits       BIGINT NOT NULL DEFAULT 0 CHECK (hits >= 0),
    updated_at TIMESTAMP NOT NULL,
    deleted_at TIMESTAMP
);

-- Nested mutations: one parent with an edge of each nestable shape. A UNIQUE
-- name is the upsert conflict target and a constraint a parent write can violate, and
-- deleted_at lets a nested write leave the parent soft-deleted.
CREATE TABLE authors (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    deleted_at TIMESTAMP
);

-- Many-to-many: a UNIQUE tag name is a constraint a nested create can violate.
CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
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
    id        INTEGER PRIMARY KEY,
    author_id BIGINT UNIQUE REFERENCES authors(id),
    bio       TEXT NOT NULL
);

-- One-to-many on a nullable foreign key, so every verb is generated. The CHECK
-- is a constraint a nested create can violate, and a soft-deleted post is a
-- connect target the visibility read hides.
CREATE TABLE posts (
    id         INTEGER PRIMARY KEY,
    author_id  BIGINT REFERENCES authors(id),
    title      TEXT NOT NULL CHECK (title <> ''),
    deleted_at TIMESTAMP
);

-- Nested writes through composite foreign keys, which composite-key parents and
-- targets cannot carry. Every van key shares region, so region is pinned and
-- depot_id and route_id are free: this is a tenant key's shape with no tenancy.
-- Route.Vans sets allow_reparent, Depot.Vans does not. van_tools is a junction
-- whose two keys share region. van_permits takes permit_code off the target.
-- lockers hold a key whose two members are both free, so a row can hold a partly
-- NULL key.
CREATE TABLE depots (
    id     INTEGER PRIMARY KEY,
    region TEXT NOT NULL,
    name   TEXT NOT NULL UNIQUE,
    UNIQUE (region, id)
);

CREATE TABLE routes (
    id     INTEGER PRIMARY KEY,
    region TEXT NOT NULL,
    name   TEXT NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE vans (
    id       INTEGER PRIMARY KEY,
    region   TEXT NOT NULL,
    depot_id BIGINT,
    route_id BIGINT,
    plate    TEXT NOT NULL,
    UNIQUE (region, id),
    FOREIGN KEY (region, depot_id) REFERENCES depots (region, id),
    FOREIGN KEY (region, route_id) REFERENCES routes (region, id)
);

CREATE TABLE tools (
    id     INTEGER PRIMARY KEY,
    region TEXT NOT NULL,
    name   TEXT NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE van_tools (
    region  TEXT NOT NULL,
    van_id  BIGINT NOT NULL,
    tool_id BIGINT NOT NULL,
    PRIMARY KEY (region, van_id, tool_id),
    FOREIGN KEY (region, van_id) REFERENCES vans (region, id),
    FOREIGN KEY (region, tool_id) REFERENCES tools (region, id)
);

CREATE TABLE permits (
    id   INTEGER PRIMARY KEY,
    code TEXT NOT NULL UNIQUE
);

CREATE TABLE van_permits (
    van_id      BIGINT NOT NULL,
    permit_code TEXT NOT NULL,
    PRIMARY KEY (van_id, permit_code),
    FOREIGN KEY (van_id) REFERENCES vans (id),
    FOREIGN KEY (permit_code) REFERENCES permits (code)
);

CREATE TABLE bays (
    id     INTEGER PRIMARY KEY,
    region TEXT NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE lockers (
    id         INTEGER PRIMARY KEY,
    bay_region TEXT,
    bay_id     BIGINT,
    label      TEXT NOT NULL,
    FOREIGN KEY (bay_region, bay_id) REFERENCES bays (region, id)
);

-- slots hold the same two free members as lockers, on an edge that sets
-- allow_reparent, so a row holding a partly NULL key is adopted.
CREATE TABLE slots (
    id         INTEGER PRIMARY KEY,
    bay_region TEXT,
    bay_id     BIGINT,
    label      TEXT NOT NULL,
    FOREIGN KEY (bay_region, bay_id) REFERENCES bays (region, id)
);

-- Edges through composite foreign keys. item_notes (O2M) and item_receipts (O2O,
-- a unique member) reach the composite primary key of order_items. parcels (O2M)
-- reach a UNIQUE pair with a nullable referenced column, through nullable
-- members. item_stations links order_items and stations through two composite
-- keys that share product_id, the shape of a tenant junction. stations carries
-- no foreign key of its own, so products keeps its edges.
CREATE TABLE item_notes (
    id         INTEGER PRIMARY KEY,
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    body       TEXT NOT NULL,
    deleted_at TIMESTAMP,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE item_receipts (
    id         INTEGER PRIMARY KEY,
    order_id   BIGINT NOT NULL UNIQUE,
    product_id BIGINT NOT NULL,
    code       TEXT NOT NULL,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE shelves (
    id     INTEGER PRIMARY KEY,
    region TEXT NOT NULL,
    bin    BIGINT,
    tag    TEXT UNIQUE,
    UNIQUE (region, bin)
);

CREATE TABLE parcels (
    id     INTEGER PRIMARY KEY,
    region TEXT,
    bin    BIGINT,
    label  TEXT NOT NULL,
    FOREIGN KEY (region, bin) REFERENCES shelves (region, bin)
);

CREATE TABLE stations (
    id         INTEGER PRIMARY KEY,
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
    id        INTEGER PRIMARY KEY,
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

-- A junction keyed by a surrogate: the link step has to target its UNIQUE.
CREATE TABLE seals (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE crate_seals (
    id       INTEGER PRIMARY KEY,
    crate_id BIGINT NOT NULL REFERENCES crates (id),
    seal_id  BIGINT NOT NULL REFERENCES seals (id),
    UNIQUE (crate_id, seal_id)
);
