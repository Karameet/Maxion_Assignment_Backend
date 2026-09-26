-- +goose Up
CREATE TABLE products (
    id          TEXT        PRIMARY KEY CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    name        TEXT        NOT NULL,
    price_cents BIGINT      NOT NULL CHECK (price_cents >= 0),
    active      BOOLEAN     NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE orders (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id       TEXT        NOT NULL REFERENCES products(id),
    quantity         INT         NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT      NOT NULL CHECK (unit_price_cents >= 0),  -- price snapshot at order time
    total_cents      BIGINT      NOT NULL CHECK (total_cents >= 0),
    idempotency_key  TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT orders_user_idem_uniq UNIQUE (user_id, idempotency_key)
);

CREATE INDEX orders_user_created_idx ON orders (user_id, created_at DESC);

-- +goose Down
DROP TABLE orders;
DROP TABLE products;
