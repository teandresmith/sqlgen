-- workspace_line_totals — a tenanted MATERIALIZED view whose tenant column
-- carries the @pk annotation. Two properties ride on this shape:
--
--   - @pk selects a Get signature; it does not make the column a DDL primary
--     key, so the view stays plain-filtered and the manifest reports
--     mode "auto-filter", never the mutation-only "verify-match" (PRD §30.4.2).
--   - Refresh / RefreshConcurrently recompute the whole relation and are never
--     tenant-scoped (§29.2.5), while reads of the refreshed matview
--     still are.
--
-- @pk: workspace_id
-- @type line_count: int64

CREATE MATERIALIZED VIEW workspace_line_totals AS
SELECT
    li.workspace_id,
    COUNT(li.product_id) AS line_count
FROM line_items li
GROUP BY li.workspace_id;
