package order

import "errors"

var (
	ErrMissingIdempotencyKey = errors.New("Idempotency-Key header is required")
	ErrInvalidIdempotencyKey = errors.New("Idempotency-Key must be 8-128 characters of [A-Za-z0-9_-]")
	ErrInvalidProductID      = errors.New("productId must be 1-64 characters of [A-Za-z0-9_-]")
	ErrInvalidQuantity       = errors.New("quantity must be a positive integer within the allowed maximum")
	ErrProductNotFound       = errors.New("product not found")
	ErrIdempotencyKeyReused  = errors.New("Idempotency-Key was already used with a different request body")
	// ErrUserNotFound means the orders.user_id FK failed: the token is valid
	// but the account no longer exists.
	ErrUserNotFound = errors.New("user not found")
)
