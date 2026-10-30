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
-- connect target the visibility read hides.
CREATE TABLE posts (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id  BIGINT NULL,
    title      VARCHAR(64) NOT NULL CHECK (title <> ''),
    deleted_at TIMESTAMP(6) NULL,
    FOREIGN KEY (author_id) REFERENCES authors(id)
);
