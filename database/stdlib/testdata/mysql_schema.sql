CREATE TABLE IF NOT EXISTS test_users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    CONSTRAINT test_users_name_check CHECK (CHAR_LENGTH(name) > 0)
);

CREATE TABLE IF NOT EXISTS test_orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT NOT NULL,
    amount INT NOT NULL,
    CONSTRAINT test_orders_user_fk FOREIGN KEY (user_id) REFERENCES test_users(id)
);
