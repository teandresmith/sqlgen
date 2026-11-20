-- A cached, tenanted view: a write to tasks clears its cache.
--
-- @pk: id

CREATE VIEW active_tasks AS
SELECT id, workspace_id, title
FROM tasks
WHERE deleted_at IS NULL;
