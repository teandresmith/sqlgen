-- @pk: project_id

CREATE MATERIALIZED VIEW project_task_counts AS
SELECT
    t.project_id,
    COUNT(t.id) AS task_count
FROM tasks t
GROUP BY t.project_id;
