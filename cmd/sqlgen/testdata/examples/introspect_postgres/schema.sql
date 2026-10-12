-- The database's current schema. sqlgen reads it by introspection
-- (input.source: both); migrations/ holds the changes not yet applied.

CREATE SCHEMA billing;

CREATE TYPE account_status AS ENUM ('active', 'suspended');

CREATE TYPE postal_address AS (
    street TEXT,
    city   TEXT
);

CREATE DOMAIN email_address AS TEXT CHECK (VALUE LIKE '%@%');

CREATE TABLE accounts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email        email_address NOT NULL,
    display_name TEXT NOT NULL,
    status       account_status NOT NULL DEFAULT 'active',
    address      postal_address,
    legacy_code  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE billing.invoices (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id     BIGINT NOT NULL REFERENCES public.accounts (id),
    subtotal_cents INTEGER NOT NULL,
    tax_cents      INTEGER NOT NULL,
    total_cents    INTEGER GENERATED ALWAYS AS (subtotal_cents + tax_cents) STORED
);

CREATE TABLE projects (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts (id),
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL,
    summary    TEXT
);

CREATE TABLE tasks (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id  BIGINT NOT NULL REFERENCES projects (id),
    assignee_id BIGINT,
    title       TEXT NOT NULL,
    done        BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE legacy_events (
    id      BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    payload TEXT NOT NULL
);

COMMENT ON TYPE account_status IS 'Lifecycle state of an account';
COMMENT ON TYPE postal_address IS 'Mailing address components';
COMMENT ON DOMAIN email_address IS 'An address containing an @';

COMMENT ON TABLE accounts IS 'Customer accounts';
COMMENT ON COLUMN accounts.email IS 'Login email address';
COMMENT ON COLUMN accounts.display_name IS 'Name shown to other users';

COMMENT ON TABLE billing.invoices IS 'Invoices issued to an account';
COMMENT ON COLUMN billing.invoices.total_cents IS 'Subtotal plus tax';
