CREATE TABLE IF NOT EXISTS test_users (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(name) > 0),
    email TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS test_orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES test_users(id),
    amount INTEGER NOT NULL
);
