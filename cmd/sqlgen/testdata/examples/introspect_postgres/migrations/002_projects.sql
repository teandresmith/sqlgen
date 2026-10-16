-- Pending: not yet applied to the database.
ALTER TABLE projects ADD CONSTRAINT projects_slug_key UNIQUE (slug);
ALTER TABLE projects ALTER COLUMN summary SET NOT NULL;
ALTER TABLE tasks ADD CONSTRAINT tasks_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES accounts (id);
