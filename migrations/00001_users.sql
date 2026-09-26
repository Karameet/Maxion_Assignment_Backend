-- +goose Up
CREATE TABLE users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id     TEXT        UNIQUE,                 -- guest
    email         TEXT        UNIQUE,                 -- stored normalized (trim + lowercase)
    password_hash TEXT,                               -- bcrypt only, never plaintext
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_identity_chk CHECK (
        device_id IS NOT NULL
        OR (email IS NOT NULL AND password_hash IS NOT NULL)
    ),
    CONSTRAINT users_email_pw_chk CHECK ((email IS NULL) = (password_hash IS NULL))
);

-- +goose Down
DROP TABLE users;
