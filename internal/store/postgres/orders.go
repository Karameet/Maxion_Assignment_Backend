package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"order-backend/internal/order"
)

const orderColumns = "id, user_id, product_id, quantity, unit_price_cents, total_cents, idempotency_key, created_at"

type OrderRepo struct {
	db *gorm.DB
}

func NewOrderRepo(db *gorm.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

func (r *OrderRepo) FindByKey(ctx context.Context, userID uuid.UUID, key string) (*order.Order, error) {
	var o order.Order
	res := r.db.WithContext(ctx).Raw(`
		SELECT `+orderColumns+` FROM orders
		WHERE user_id = $1 AND idempotency_key = $2`, userID, key).Scan(&o)
	if res.Error != nil {
		return nil, fmt.Errorf("find order by key: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &o, nil
}

// InsertIfAbsent relies on UNIQUE (user_id, idempotency_key). A concurrent
// INSERT with the same key blocks until the first transaction commits, then
// DO NOTHING returns no row; the follow-up SELECT (a new statement under
// READ COMMITTED) is guaranteed to see the committed winner.
func (r *OrderRepo) InsertIfAbsent(ctx context.Context, n order.NewOrder) (*order.Order, bool, error) {
	var o order.Order
	res := r.db.WithContext(ctx).Raw(`
		INSERT INTO orders (user_id, product_id, quantity, unit_price_cents, total_cents, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, idempotency_key) DO NOTHING
		RETURNING `+orderColumns,
		n.UserID, n.ProductID, n.Quantity, n.UnitPriceCents, n.TotalCents, n.IdempotencyKey).Scan(&o)
	if pgErrorIs(res.Error, pgForeignKeyViolation, "orders_user_id_fkey") {
		return nil, false, order.ErrUserNotFound
	}
	if res.Error != nil {
		return nil, false, fmt.Errorf("insert order: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return &o, true, nil
	}

	existing, err := r.FindByKey(ctx, n.UserID, n.IdempotencyKey)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, fmt.Errorf("insert order: conflict but no existing row for key")
	}
	return existing, false, nil
}

func (r *OrderRepo) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]order.Order, error) {
	var os []order.Order
	err := r.db.WithContext(ctx).Raw(`
		SELECT `+orderColumns+` FROM orders
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, userID, limit).Scan(&os).Error
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	return os, nil
}
