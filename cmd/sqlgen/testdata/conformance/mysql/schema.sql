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
