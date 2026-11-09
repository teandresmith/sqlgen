-- tenancy_postgres: pgx leg for tenant capture (uuid tenant type,
-- native RETURNING). Covers the single-numeric-PK and composite-PK
-- (tenant outside the PK) shapes; the pgx-batch UpdateMany capture is the
-- path this module exists to compile and run.

CREATE TABLE articles (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    title TEXT NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE line_items (
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    workspace_id UUID NOT NULL,
    quantity BIGINT NOT NULL,
    PRIMARY KEY (order_id, product_id)
);

-- Composite tenant foreign keys. Each child key carries workspace_id, so a row
-- cannot reference a parent in another workspace. tasks.workspace_id is the
-- tenant column and a member of three keys, so no edge rewrites it.
CREATE TABLE cycles (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (workspace_id, id)
);

-- Sprint.Tasks sets allow_reparent, Cycle.Tasks does not.
CREATE TABLE sprints (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (workspace_id, id)
);

-- A composite-PK target: it has every read surface and no nested writes.
CREATE TABLE projects (
    workspace_id UUID NOT NULL,
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (workspace_id, code)
);

CREATE TABLE tasks (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    cycle_id BIGINT,
    sprint_id BIGINT,
    project_code TEXT,
    title TEXT NOT NULL,
    UNIQUE (workspace_id, id),
    FOREIGN KEY (workspace_id, cycle_id) REFERENCES cycles (workspace_id, id),
    FOREIGN KEY (workspace_id, sprint_id) REFERENCES sprints (workspace_id, id),
    FOREIGN KEY (workspace_id, project_code) REFERENCES projects (workspace_id, code)
);

CREATE TABLE labels (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (workspace_id, id)
);

-- A tenant junction: its two keys share workspace_id, and its primary key is
-- their union.
CREATE TABLE task_labels (
    workspace_id UUID NOT NULL,
    task_id BIGINT NOT NULL,
    label_id BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, task_id, label_id),
    FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
    FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id)
);

-- A tenant junction keyed by a surrogate: the link step targets the UNIQUE over
-- the union of its two keys, and the edge has no `disconnect`.
CREATE TABLE reviewers (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (workspace_id, id)
);

CREATE TABLE task_reviewers (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    task_id BIGINT NOT NULL,
    reviewer_id BIGINT NOT NULL,
    UNIQUE (workspace_id, task_id, reviewer_id),
    FOREIGN KEY (workspace_id, task_id) REFERENCES tasks (workspace_id, id),
    FOREIGN KEY (workspace_id, reviewer_id) REFERENCES reviewers (workspace_id, id)
);

-- A junction into a UNIQUE column: the link row takes badge_code off the target.
CREATE TABLE badges (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    code TEXT NOT NULL UNIQUE
);

CREATE TABLE task_badges (
    task_id BIGINT NOT NULL REFERENCES tasks (id),
    badge_code TEXT NOT NULL REFERENCES badges (code),
    PRIMARY KEY (task_id, badge_code)
);

-- A MATCH FULL key whose members are all free and nullable: unlinking nulls
-- both, since PostgreSQL refuses a key that mixes NULL and non-NULL members.
CREATE TABLE milestones (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    code TEXT NOT NULL,
    UNIQUE (code, id)
);

CREATE TABLE checkpoints (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    milestone_code TEXT,
    milestone_id BIGINT,
    title TEXT NOT NULL,
    FOREIGN KEY (milestone_code, milestone_id) REFERENCES milestones (code, id) MATCH FULL
);

-- A shared parent holding a key into a tenanted child: template_steps takes its
-- workspace_id from owner_workspace, which tenancy does not guard.
CREATE TABLE templates (
    id BIGSERIAL PRIMARY KEY,
    owner_workspace UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (owner_workspace, id)
);

CREATE TABLE template_steps (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    template_id BIGINT NOT NULL,
    title TEXT NOT NULL,
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id)
);

-- The nullable half of template_steps: the key's template_id is free, so the
-- edge keeps `disconnect` and `clear` under a shared parent.
CREATE TABLE template_notes (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    template_id BIGINT,
    body TEXT NOT NULL,
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id)
);

-- A tenant junction under a shared parent: its workspace_id comes from
-- owner_workspace and from the resolver.
CREATE TABLE template_labels (
    workspace_id UUID NOT NULL,
    template_id BIGINT NOT NULL,
    label_id BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, template_id, label_id),
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id),
    FOREIGN KEY (workspace_id, label_id) REFERENCES labels (workspace_id, id)
);

-- A tenant junction between two shared tables: the keys agree on workspace_id,
-- and only tenancy's own check can find that the resolver does not.
CREATE TABLE stamps (
    id BIGSERIAL PRIMARY KEY,
    owner_workspace UUID NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (owner_workspace, id)
);

CREATE TABLE template_stamps (
    workspace_id UUID NOT NULL,
    template_id BIGINT NOT NULL,
    stamp_id BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, template_id, stamp_id),
    FOREIGN KEY (workspace_id, template_id) REFERENCES templates (owner_workspace, id),
    FOREIGN KEY (workspace_id, stamp_id) REFERENCES stamps (owner_workspace, id)
);

-- A single-column FK on the tenant column, from a shared parent.
CREATE TABLE workspaces (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL
);

CREATE TABLE workspace_notes (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL REFERENCES workspaces (id),
    body TEXT NOT NULL
);

-- Two free members under MATCH SIMPLE, so a row can hold a partly NULL key.
CREATE TABLE zones (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    region TEXT NOT NULL,
    UNIQUE (region, id)
);

CREATE TABLE sites (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    zone_region TEXT,
    zone_id BIGINT,
    FOREIGN KEY (zone_region, zone_id) REFERENCES zones (region, id)
);

-- An edge that sets allow_reparent over a key with two free members, so a row
-- holding a partly NULL key is adopted rather than refused as sites' is.
CREATE TABLE plots (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    zone_region TEXT,
    zone_id BIGINT,
    FOREIGN KEY (zone_region, zone_id) REFERENCES zones (region, id)
);

-- A nullable member two keys share: org is pinned on both edges, and a row can
-- hold NULL there. SharedGuardian.SharedKids sets allow_reparent.
CREATE TABLE shared_parents (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    org TEXT NOT NULL,
    UNIQUE (org, id)
);

CREATE TABLE shared_guardians (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    org TEXT NOT NULL,
    UNIQUE (org, id)
);

CREATE TABLE shared_kids (
    id BIGSERIAL PRIMARY KEY,
    workspace_id UUID NOT NULL,
    org TEXT,
    parent_id BIGINT,
    guardian_id BIGINT,
    FOREIGN KEY (org, parent_id) REFERENCES shared_parents (org, id),
    FOREIGN KEY (org, guardian_id) REFERENCES shared_guardians (org, id)
);
