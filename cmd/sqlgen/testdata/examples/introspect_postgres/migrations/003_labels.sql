-- Pending: not yet applied to the database.
CREATE TYPE task_priority AS ENUM ('low', 'normal', 'high');

ALTER TABLE tasks ADD COLUMN priority task_priority NOT NULL DEFAULT 'normal';

CREATE TABLE task_labels (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id BIGINT NOT NULL REFERENCES tasks (id),
    label   TEXT NOT NULL
);

DROP TABLE legacy_events;
