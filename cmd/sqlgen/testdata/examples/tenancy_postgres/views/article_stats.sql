-- article_stats — a tenanted regular view on the pgx leg. Scoped by detection
-- alone (no `views.article_stats.tenancy` block), @pk makes Get available so
-- all five read methods are exercised against a real PostgreSQL server.
--
-- @pk: id

CREATE VIEW article_stats AS
SELECT
    a.id,
    a.workspace_id,
    a.title
FROM articles a
WHERE a.deleted_at IS NULL;
