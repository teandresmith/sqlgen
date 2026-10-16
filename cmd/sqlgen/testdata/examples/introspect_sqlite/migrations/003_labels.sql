-- Pending: not yet applied to the database.
CREATE TABLE task_labels (
    id      INTEGER PRIMARY KEY,
    task_id INTEGER NOT NULL REFERENCES tasks (id),
    label   TEXT NOT NULL
);

DROP TABLE legacy_events;
