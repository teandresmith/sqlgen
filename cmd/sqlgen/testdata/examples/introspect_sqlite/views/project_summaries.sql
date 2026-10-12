-- @pk: id

CREATE VIEW project_summaries AS
SELECT
    p.id,
    p.name,
    p.slug
FROM projects p;
