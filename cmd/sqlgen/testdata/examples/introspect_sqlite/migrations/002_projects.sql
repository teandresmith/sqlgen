-- Pending: not yet applied to the database.
CREATE UNIQUE INDEX projects_slug_key ON projects (slug);
ALTER TABLE tasks ADD COLUMN assignee_id INTEGER REFERENCES accounts (id);
