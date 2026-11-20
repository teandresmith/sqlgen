-- The Features schema (docs/design/CONFORMANCE_VARIANTS.md §5.1): one table per
-- tenancy, cache and event carrier shape. The tenant column is workspace_id, a
-- uuid.UUID on PostgreSQL and SQLite and a CHAR(36) string here. The suite
-- reaches it through the models.Tenant alias, so the rest of the generated Go
-- API matches field for field. Every integer is BIGINT and every surrogate key
-- is database-generated.

-- The tenant outside the primary key, on a database-generated key. The unique
-- slug gives Upsert a conflict target with an update branch, and views gives
-- it an Increment.
CREATE TABLE notes (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    slug         VARCHAR(64) NOT NULL,
    body         VARCHAR(64) NOT NULL,
    views        BIGINT NOT NULL DEFAULT 0,
    UNIQUE (workspace_id, slug)
);

-- The tenant inside a composite primary key. seats gives it an Increment.
CREATE TABLE memberships (
    workspace_id CHAR(36) NOT NULL,
    member       VARCHAR(64) NOT NULL,
    role         VARCHAR(64) NOT NULL,
    seats        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, member)
);

-- A tenanted soft delete.
CREATE TABLE tasks (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    title        VARCHAR(64) NOT NULL,
    deleted_at   TIMESTAMP(6) NULL
);

-- A tenanted parent with an O2M child and a tenant junction. Each child key
-- carries workspace_id, so a row cannot reference a parent in another
-- workspace.
CREATE TABLE boards (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    name         VARCHAR(64) NOT NULL,
    UNIQUE (workspace_id, id)
);

CREATE TABLE cards (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    board_id     BIGINT NULL,
    title        VARCHAR(64) NOT NULL,
    FOREIGN KEY (workspace_id, board_id) REFERENCES boards (workspace_id, id)
);

CREATE TABLE tags (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    name         VARCHAR(64) NOT NULL,
    UNIQUE (workspace_id, id)
);

-- Its two keys share workspace_id, and its primary key is their union.
CREATE TABLE board_tags (
    workspace_id CHAR(36) NOT NULL,
    board_id     BIGINT NOT NULL,
    tag_id       BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, board_id, tag_id),
    FOREIGN KEY (workspace_id, board_id) REFERENCES boards (workspace_id, id),
    FOREIGN KEY (workspace_id, tag_id) REFERENCES tags (workspace_id, id)
);

-- A shared parent: its children take workspace_id from owner_workspace, which
-- tenancy does not guard, so a child key can differ from the resolved tenant.
CREATE TABLE templates (
    id              BIGINT AUTO_INCREMENT PRIMARY KEY,
    owner_workspace CHAR(36) NOT NULL,
    name            VARCHAR(64) NOT NULL,
    UNIQUE (owner_workspace, id)
);

-- An O2M child whose key's template_id is free, so the edge keeps disconnect
-- and clear under the shared parent.
CREATE TABLE template_notes (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    template_id  BIGINT NULL,
    body         VARCHAR(64) NOT NULL,
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id)
);

-- A tenant junction under the shared parent: its workspace_id comes from
-- owner_workspace and from the resolver.
CREATE TABLE template_tags (
    workspace_id CHAR(36) NOT NULL,
    template_id  BIGINT NOT NULL,
    tag_id       BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, template_id, tag_id),
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id),
    FOREIGN KEY (workspace_id, tag_id) REFERENCES tags (workspace_id, id)
);

-- A tenanted table whose tenant is optional (tenancy.required: false).
CREATE TABLE bulletins (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    workspace_id CHAR(36) NOT NULL,
    body         VARCHAR(64) NOT NULL
);

-- An untenanted cached table.
CREATE TABLE regions (
    id   BIGINT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(64) NOT NULL
);
