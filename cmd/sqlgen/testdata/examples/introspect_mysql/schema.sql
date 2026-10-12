-- The database's current schema. sqlgen reads it by introspection
-- (input.source: both); migrations/ holds the changes not yet applied.

CREATE TABLE accounts (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    email        VARCHAR(255) NOT NULL COMMENT 'Login email address',
    display_name VARCHAR(100) NOT NULL COMMENT 'Name shown to other users',
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    legacy_code  VARCHAR(32),
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) COMMENT = 'Customer accounts';

CREATE TABLE projects (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    account_id BIGINT UNSIGNED NOT NULL,
    name       VARCHAR(100) NOT NULL,
    slug       VARCHAR(100) NOT NULL,
    summary    VARCHAR(255),
    tags       SET('internal', 'billing', 'urgent') NOT NULL DEFAULT '',
    CONSTRAINT projects_account_id_fkey FOREIGN KEY (account_id) REFERENCES accounts (id)
);

CREATE TABLE tasks (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    project_id       BIGINT UNSIGNED NOT NULL,
    assignee_id      BIGINT UNSIGNED,
    title            VARCHAR(200) NOT NULL,
    estimate_minutes INT UNSIGNED NOT NULL DEFAULT 0,
    estimate_seconds INT UNSIGNED GENERATED ALWAYS AS (estimate_minutes * 60) STORED,
    CONSTRAINT tasks_project_id_fkey FOREIGN KEY (project_id) REFERENCES projects (id)
);

CREATE TABLE legacy_events (
    id      BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    payload TEXT NOT NULL
);
