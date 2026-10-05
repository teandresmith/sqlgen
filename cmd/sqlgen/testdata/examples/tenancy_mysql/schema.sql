-- tenancy_mysql: MySQL leg for tenant capture. The tenant column is
-- CHAR(36) (uuid.UUID's driver.Valuer emits the canonical string form) and is
-- NOT NULL on every tenanted table per PRD §29.2.

-- Single numeric PK, tenant OUTSIDE the PK. Soft delete + a unique key (for
-- upsert's conflict target) + a numeric column (for increment) so every
-- MySQL pre-read shape is generated: single-row, batched *Many,
-- *Where collect, upsert post-read, increment pre-read.
CREATE TABLE articles (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    workspace_id CHAR(36) NOT NULL,
    slug VARCHAR(64) NOT NULL,
    title VARCHAR(255) NOT NULL,
    view_count BIGINT NOT NULL DEFAULT 0,
    deleted_at TIMESTAMP NULL,
    UNIQUE KEY uq_articles_workspace_slug (workspace_id, slug)
);

-- Composite PK with the tenant OUTSIDE the PK — exercises the PK-struct-keyed
-- captureAffectedTenants map and the widened collectAffectedPKs.
CREATE TABLE line_items (
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    workspace_id CHAR(36) NOT NULL,
    quantity BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

-- String-typed PK, tenant outside the PK — exercises the pkIsStringType
-- variants of the widened collect and the capture map.
CREATE TABLE documents (
    doc_key VARCHAR(64) NOT NULL,
    workspace_id CHAR(36) NOT NULL,
    label VARCHAR(255) NOT NULL,
    PRIMARY KEY (doc_key)
);
