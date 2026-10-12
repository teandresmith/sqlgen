-- Runs after views/. The unique index backing project_task_counts' @pk,
-- which REFRESH MATERIALIZED VIEW CONCURRENTLY requires.
CREATE UNIQUE INDEX project_task_counts_project_id_idx ON project_task_counts (project_id);
