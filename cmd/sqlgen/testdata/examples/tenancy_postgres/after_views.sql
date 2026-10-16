-- Runs after views/. REFRESH MATERIALIZED VIEW CONCURRENTLY requires a UNIQUE
-- index covering all rows; the @pk annotation asserts it generator-side, the
-- server needs the real index.
CREATE UNIQUE INDEX workspace_line_totals_workspace_id_idx ON workspace_line_totals (workspace_id);
