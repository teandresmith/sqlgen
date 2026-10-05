-- workspace_note_summary — the view proof fixture: a TENANTED view reached
-- over the real gqlgen server. It projects `workspace_id`, so §29.2.5's
-- detection rule scopes every one of its reads with no
-- `views.workspace_note_summary.tenancy` block; `@pk` makes the by-PK query
-- available, and `id` satisfies the inherited `cursor_keys` default so the
-- connection query is emitted too. All three read queries therefore exist,
-- which is what lets tests/view_tenancy_test.go prove isolation across the
-- whole read surface rather than one query of it.
--
-- `kind` (ENUM) and `labels` (JSONB) are projected so <V>Filter carries the
-- enum and JSONB comparator families — PRD §26.4 promises a view's filter is
-- the table's read half, and nothing else in the example tree tests that
-- claim on a view.
--
-- The LEFT JOIN onto documents is what makes this a real view rather than a
-- passthrough: `document_count` is an aggregate, so the object type carries a
-- column with no base table behind it. `documents` is the schema's polymorphic
-- child table — `entity_id` carries no FK precisely so several parents can
-- share it — and `spv` is the one member of document_entity_type_enum that is
-- NOT asset-scoped, so notes claim it without disturbing the §13.7
-- sub-categorized-polymorphism fixture the `asset.*` members serve.
--
-- @pk: id
-- @type document_count: int32

CREATE VIEW workspace_note_summary AS
SELECT
    n.id,
    n.workspace_id,
    n.kind,
    n.labels,
    n.body,
    n.pinned_order,
    COUNT(d.id) AS document_count
FROM workspace_notes n
LEFT JOIN documents d
       ON d.entity_id = n.id AND d.entity_type = 'spv'
GROUP BY n.id, n.workspace_id, n.kind, n.labels, n.body, n.pinned_order;
