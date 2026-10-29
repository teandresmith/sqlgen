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
