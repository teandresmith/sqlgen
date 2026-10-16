-- Pending: not yet applied to the database.
ALTER TABLE accounts ADD COLUMN nickname VARCHAR(100);
ALTER TABLE accounts ADD COLUMN locale VARCHAR(10) NOT NULL DEFAULT 'en';
ALTER TABLE accounts DROP COLUMN legacy_code;
