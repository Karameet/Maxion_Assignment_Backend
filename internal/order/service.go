package order

import (
	"context"
	"regexp"

	"github.com/google/uuid"
)

var (
	idempotencyKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	productIDRe      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

const listOrdersLimit = 50

type Service struct {
	products    ProductRepository
	orders      OrderRepository
	maxQuantity int
}

func NewService(products ProductRepository, orders OrderRepository, maxQuantity int) *Service {
	return &Service{products: products, orders: orders, maxQuantity: maxQuantity}
}

type CreateOrderInput struct {
	UserID         uuid.UUID
	IdempotencyKey string
	ProductID      string
	// Quantity is already parsed from a strict JSON integer by the handler;
	// anything that was not an integer arrives here as 0 and is rejected.
	Quantity int64
}

type CreateOrderResult struct {
	Order    *Order
	Replayed bool
}

// ValidateIdempotencyKey is exported so the handler can reject a bad key
// before parsing the body (the documented validation order).
func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return ErrMissingIdempotencyKey
	}
	if !idempotencyKeyRe.MatchString(key) {
		return ErrInvalidIdempotencyKey
	}
	return nil
}

func (s *Service) validate(in CreateOrderInput) error {
	if err := ValidateIdempotencyKey(in.IdempotencyKey); err != nil {
		return err
	}
	if !productIDRe.MatchString(in.ProductID) {
		return ErrInvalidProductID
	}
	if in.Quantity < 1 || in.Quantity > int64(s.maxQuantity) {
		return ErrInvalidQuantity
	}
	return nil
}

// CreateOrder validates input, prices the order from server-side data only,
// and guarantees at most one order per (user, idempotency key) — including
// under concurrent requests, where the repository's unique constraint picks
// the winner.
func (s *Service) CreateOrder(ctx context.Context, in CreateOrderInput) (*CreateOrderResult, error) {
	if err := s.validate(in); err != nil {
		return nil, err
	}
	qty := int(in.Quantity)

	// Fast replay path: the key was used before.
	existing, err := s.orders.FindByKey(ctx, in.UserID, in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return replay(existing, in.ProductID, qty)
	}

	product, err := s.products.FindActive(ctx, in.ProductID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, ErrProductNotFound // key is not consumed; client may fix and retry
	}

	// qty ≤ maxQuantity keeps this far from int64 overflow.
	total := product.PriceCents * int64(qty)

	o, created, err := s.orders.InsertIfAbsent(ctx, NewOrder{
		UserID:         in.UserID,
		ProductID:      product.ID,
		Quantity:       qty,
		UnitPriceCents: product.PriceCents,
		TotalCents:     total,
		IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	if !created {
		// Lost the race to a concurrent request with the same key.
		return replay(o, in.ProductID, qty)
	}
	return &CreateOrderResult{Order: o, Replayed: false}, nil
}

// replay returns the stored order if the request fingerprint matches, so a
// retry sees exactly what the first attempt produced.
func replay(o *Order, productID string, qty int) (*CreateOrderResult, error) {
	if o.ProductID != productID || o.Quantity != qty {
		return nil, ErrIdempotencyKeyReused
	}
	return &CreateOrderResult{Order: o, Replayed: true}, nil
}

func (s *Service) ListProducts(ctx context.Context) ([]Product, error) {
	return s.products.ListActive(ctx)
}

func (s *Service) ListOrders(ctx context.Context, userID uuid.UUID) ([]Order, error) {
	return s.orders.ListByUser(ctx, userID, listOrdersLimit)
}
