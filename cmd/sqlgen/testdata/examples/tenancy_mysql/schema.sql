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

-- Composite tenant foreign keys under nested writes. tasks.workspace_id is the
-- tenant column and a member of the key, so no edge rewrites it.
CREATE TABLE cycles (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    UNIQUE (workspace_id, id)
);

CREATE TABLE tasks (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    cycle_id BIGINT,
    title VARCHAR(255) NOT NULL,
    FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id)
);

CREATE TABLE labels (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    UNIQUE (workspace_id, id)
);

-- A shared parent holding a key into tenanted children: their workspace_id comes
-- from owner_workspace, which tenancy does not guard, and from the resolver.
CREATE TABLE templates (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    owner_workspace CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    UNIQUE (owner_workspace, id)
);

CREATE TABLE template_steps (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    template_id BIGINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id)
);

CREATE TABLE template_labels (
    workspace_id CHAR(36) NOT NULL,
    template_id BIGINT NOT NULL,
    label_id BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, template_id, label_id),
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id),
    FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id)
);

-- A tenant junction between two shared tables: the keys agree on workspace_id,
-- and only the tenancy check can find that the resolver does not.
CREATE TABLE stamps (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    owner_workspace CHAR(36) NOT NULL,
    name VARCHAR(255) NOT NULL,
    UNIQUE (owner_workspace, id)
);

CREATE TABLE template_stamps (
    workspace_id CHAR(36) NOT NULL,
    template_id BIGINT NOT NULL,
    stamp_id BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, template_id, stamp_id),
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id),
    FOREIGN KEY (workspace_id, stamp_id) REFERENCES stamps (owner_workspace, id)
);

-- A single-column FK on the tenant column, from a shared parent.
CREATE TABLE workspaces (
    id CHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE workspace_notes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    body VARCHAR(255) NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces (id)
);

-- moments: a time-keyed nested target. The executor buckets the ids a
-- connect or disconnect names by instant, so a key in another zone than the
-- row the driver scans back still names that row. moment_owners carries O2M
-- over the nullable owner_id, moment_tags M2M over moment_tag_links.
CREATE TABLE moment_owners (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE moments (
    at DATETIME(6) PRIMARY KEY,
    owner_id BIGINT,
    label VARCHAR(255) NOT NULL,
    FOREIGN KEY (owner_id) REFERENCES moment_owners (id)
);

CREATE TABLE moment_tags (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE moment_tag_links (
    tag_id BIGINT NOT NULL,
    moment_at DATETIME(6) NOT NULL,
    PRIMARY KEY (tag_id, moment_at),
    FOREIGN KEY (tag_id) REFERENCES moment_tags (id),
    FOREIGN KEY (moment_at) REFERENCES moments (at)
);
