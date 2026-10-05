-- article_stats — the MySQL leg of the tenanted-view read-path pin. The tenant
-- column is CHAR(36) here rather than uuid, so the predicate is exercised
-- against a string-typed tenant (PRD §29.2.4 uniform-type rule).
--
-- @pk: id

CREATE VIEW article_stats AS
SELECT
    a.id,
    a.workspace_id,
    a.slug,
    a.title,
    a.view_count
FROM articles a
WHERE a.deleted_at IS NULL;
