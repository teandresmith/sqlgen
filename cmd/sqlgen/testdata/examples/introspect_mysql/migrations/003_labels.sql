-- Pending: not yet applied to the database.
ALTER TABLE tasks ADD COLUMN priority ENUM('low', 'normal', 'high') NOT NULL DEFAULT 'normal';

CREATE TABLE task_labels (
    id      BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    task_id BIGINT UNSIGNED NOT NULL,
    label   VARCHAR(50) NOT NULL,
    CONSTRAINT task_labels_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks (id)
);

DROP TABLE legacy_events;
