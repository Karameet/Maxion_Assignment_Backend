-- Runs once, on first start of an empty volume. Integration tests TRUNCATE
-- tables, so they get their own database and never touch dev data.
CREATE DATABASE orders_test OWNER orders;
