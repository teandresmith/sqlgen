-- Pending: not yet applied to the database.
ALTER TABLE accounts ADD COLUMN nickname TEXT;
ALTER TABLE accounts ADD COLUMN locale TEXT NOT NULL DEFAULT 'en';
ALTER TABLE accounts DROP COLUMN legacy_code;
