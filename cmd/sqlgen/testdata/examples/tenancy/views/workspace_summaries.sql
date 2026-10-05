-- workspace_summaries — the deliberate cross-tenant aggregate. It projects
-- workspace_id (so detection would scope it) but carries
-- `views.workspace_summaries.tenancy.enabled: false`, the §29.2.5 opt-out.
-- Pins that an opted-out view regenerates with no tenancy emission at all.
--
-- @type product_count: int64

CREATE VIEW workspace_summaries AS
SELECT
    w.id AS workspace_id,
    w.name AS workspace_name,
    COUNT(p.id) AS product_count
FROM workspaces w
LEFT JOIN products p ON p.workspace_id = w.id
GROUP BY w.id, w.name;
