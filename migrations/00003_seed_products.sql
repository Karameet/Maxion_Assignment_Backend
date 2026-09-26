-- +goose Up
INSERT INTO products (id, name, price_cents) VALUES
    ('product-123', 'Health Potion', 9900),    -- 2 x = 198.00, matches the assignment example
    ('product-456', 'Mana Potion',   4950),
    ('sword-001',   'Iron Sword',   25000),
    ('retired-001', 'Old Item',      1000);
UPDATE products SET active = false WHERE id = 'retired-001';

-- +goose Down
DELETE FROM products WHERE id IN ('product-123','product-456','sword-001','retired-001');
