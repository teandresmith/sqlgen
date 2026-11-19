-- The conformance schema (docs/design/CONFORMANCE.md §4.1). Every integer is
-- BIGINT and every surrogate key is database-generated, so the generated Go API
-- matches the other dialects' field for field. The foreign key is a table
-- constraint because MySQL 8.x parses an inline column REFERENCES and ignores it.

CREATE TABLE products (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    name       TEXT NOT NULL,
    price      BIGINT NOT NULL,
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
);

CREATE TABLE order_items (
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    quantity   BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id),
    FOREIGN KEY (product_id) REFERENCES products(id)
);

CREATE TABLE documents (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    title      TEXT NOT NULL,
    deleted_at TIMESTAMP(6) NULL,
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
    deleted_at  TIMESTAMP(6) NULL,
    PRIMARY KEY (document_id, number)
);

-- A case-insensitive key (the utf8mb4_0900_ai_ci collation): the database
-- matches a listed key that differs from the stored one only in case.
CREATE TABLE labels (
    name       VARCHAR(64) COLLATE utf8mb4_0900_ai_ci PRIMARY KEY,
    deleted_at TIMESTAMP(6) NULL
);

-- Increment, the not-null and unique mappings on an insert, and a table with
-- strict_updates off. updated_at has no DEFAULT, so an insert that leaves it out
-- violates NOT NULL.
CREATE TABLE counters (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(64) NOT NULL UNIQUE,
    hits       BIGINT NOT NULL DEFAULT 0 CHECK (hits >= 0),
    updated_at TIMESTAMP(6) NOT NULL,
    deleted_at TIMESTAMP(6) NULL
);

-- Nested mutations: one parent with an edge of each nestable shape. A UNIQUE
-- name is the upsert conflict target and a constraint a parent write can violate, and
-- deleted_at lets a nested write leave the parent soft-deleted.
CREATE TABLE authors (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(64) NOT NULL UNIQUE,
    deleted_at TIMESTAMP(6) NULL
);

-- Many-to-many: a UNIQUE tag name is a constraint a nested create can violate.
CREATE TABLE tags (
    id   BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL UNIQUE
);

-- A pure junction: its key is both foreign keys and it has no other column.
CREATE TABLE author_tags (
    author_id BIGINT NOT NULL,
    tag_id    BIGINT NOT NULL,
    PRIMARY KEY (author_id, tag_id),
    FOREIGN KEY (author_id) REFERENCES authors(id),
    FOREIGN KEY (tag_id) REFERENCES tags(id)
);

-- Has-one: a UNIQUE nullable foreign key on the child.
CREATE TABLE author_profiles (
    id        BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id BIGINT NULL UNIQUE,
    bio       TEXT NOT NULL,
    FOREIGN KEY (author_id) REFERENCES authors(id)
);

-- One-to-many on a nullable foreign key, so every verb is generated. The CHECK
-- is a constraint a nested create can violate, and a soft-deleted post is a
-- connect target the visibility read hides. kind is the discriminator of the
-- Drafts edge. A post written without it is on no such edge.
CREATE TABLE posts (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id  BIGINT NULL,
    title      VARCHAR(64) NOT NULL CHECK (title <> ''),
    kind       VARCHAR(16) NULL,
    deleted_at TIMESTAMP(6) NULL,
    FOREIGN KEY (author_id) REFERENCES authors(id)
);

-- Nested writes through composite foreign keys, which composite-key parents and
-- targets cannot carry. Every van key shares region, so region is pinned and
-- depot_id and route_id are free: this is a tenant key's shape with no tenancy.
-- Route.Vans sets allow_reparent, Depot.Vans does not. van_tools is a junction
-- whose two keys share region. van_permits takes permit_code off the target.
-- lockers hold a key whose two members are both free, so a row can hold a partly
-- NULL key.
CREATE TABLE depots (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64) NOT NULL,
    name   VARCHAR(64) NOT NULL UNIQUE,
    UNIQUE (region, id)
);

CREATE TABLE routes (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64) NOT NULL,
    name   VARCHAR(64) NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE vans (
    id       BIGINT AUTO_INCREMENT PRIMARY KEY,
    region   VARCHAR(64) NOT NULL,
    depot_id BIGINT,
    route_id BIGINT,
    plate    VARCHAR(64) NOT NULL,
    UNIQUE (region, id),
    FOREIGN KEY (region, depot_id) REFERENCES depots (region, id),
    FOREIGN KEY (region, route_id) REFERENCES routes (region, id)
);

CREATE TABLE tools (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64) NOT NULL,
    name   VARCHAR(64) NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE van_tools (
    region  VARCHAR(64) NOT NULL,
    van_id  BIGINT NOT NULL,
    tool_id BIGINT NOT NULL,
    PRIMARY KEY (region, van_id, tool_id),
    FOREIGN KEY (region, van_id) REFERENCES vans (region, id),
    FOREIGN KEY (region, tool_id) REFERENCES tools (region, id)
);

CREATE TABLE permits (
    id   BIGINT AUTO_INCREMENT PRIMARY KEY,
    code VARCHAR(64) NOT NULL UNIQUE
);

CREATE TABLE van_permits (
    van_id      BIGINT NOT NULL,
    permit_code VARCHAR(64) NOT NULL,
    PRIMARY KEY (van_id, permit_code),
    FOREIGN KEY (van_id) REFERENCES vans (id),
    FOREIGN KEY (permit_code) REFERENCES permits (code)
);

CREATE TABLE bays (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64) NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE lockers (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    bay_region VARCHAR(64),
    bay_id     BIGINT,
    label      VARCHAR(64) NOT NULL,
    FOREIGN KEY (bay_region, bay_id) REFERENCES bays (region, id)
);

-- slots hold the same two free members as lockers, on an edge that sets
-- allow_reparent, so a row holding a partly NULL key is adopted.
CREATE TABLE slots (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    bay_region VARCHAR(64),
    bay_id     BIGINT,
    label      VARCHAR(64) NOT NULL,
    FOREIGN KEY (bay_region, bay_id) REFERENCES bays (region, id)
);

-- Edges through composite foreign keys. item_notes (O2M) and item_receipts (O2O,
-- a unique member) reach the composite primary key of order_items. parcels (O2M)
-- reach a UNIQUE pair with a nullable referenced column, through nullable
-- members. item_stations links order_items and stations through two composite
-- keys that share product_id, the shape of a tenant junction. stations carries
-- no foreign key of its own, so products keeps its edges.
CREATE TABLE item_notes (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    order_id   BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    body       VARCHAR(64) NOT NULL,
    deleted_at TIMESTAMP(6) NULL,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE item_receipts (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    order_id   BIGINT NOT NULL UNIQUE,
    product_id BIGINT NOT NULL,
    code       VARCHAR(64) NOT NULL,
    FOREIGN KEY (order_id, product_id) REFERENCES order_items (order_id, product_id)
);

CREATE TABLE shelves (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64) NOT NULL,
    bin    BIGINT,
    tag    VARCHAR(64) UNIQUE,
    UNIQUE (region, bin)
);

CREATE TABLE parcels (
    id     BIGINT AUTO_INCREMENT PRIMARY KEY,
    region VARCHAR(64),
    bin    BIGINT,
    label  VARCHAR(64) NOT NULL,
    FOREIGN KEY (region, bin) REFERENCES shelves (region, bin)
);

CREATE TABLE stations (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    product_id BIGINT NOT NULL,
    name       VARCHAR(64) NOT NULL,
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
    id        BIGINT AUTO_INCREMENT PRIMARY KEY,
    shelf_tag VARCHAR(64) REFERENCES shelves (tag),
    label     VARCHAR(64) NOT NULL
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
    id   BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL
);

CREATE TABLE crate_seals (
    id       BIGINT AUTO_INCREMENT PRIMARY KEY,
    crate_id BIGINT NOT NULL,
    seal_id  BIGINT NOT NULL,
    UNIQUE (crate_id, seal_id),
    FOREIGN KEY (crate_id) REFERENCES crates (id),
    FOREIGN KEY (seal_id) REFERENCES seals (id)
);
