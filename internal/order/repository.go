package order

import (
	"context"

	"github.com/google/uuid"
)

type ProductRepository interface {
	// FindActive returns (nil, nil) when the product does not exist or is inactive.
	FindActive(ctx context.Context, id string) (*Product, error)
	ListActive(ctx context.Context) ([]Product, error)
}

type OrderRepository interface {
	// FindByKey returns (nil, nil) when the user has no order with that key.
	FindByKey(ctx context.Context, userID uuid.UUID, key string) (*Order, error)
	// InsertIfAbsent atomically inserts unless (user_id, idempotency_key)
	// already exists. created=false means another request owns the key and
	// the returned order is the existing one. Returns ErrUserNotFound on
	// user FK violation.
	InsertIfAbsent(ctx context.Context, o NewOrder) (order *Order, created bool, err error)
	ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]Order, error)
}
