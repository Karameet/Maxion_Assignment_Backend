// Package memory is a thread-safe in-memory implementation of the order
// repositories, used by unit tests and by ORDER_REPO=memory demo mode.
// A single mutex around check-and-insert emulates the Postgres
// UNIQUE (user_id, idempotency_key) constraint.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"order-backend/internal/order"
)

// DefaultProducts mirrors migrations/00003_seed_products.sql.
func DefaultProducts() []order.Product {
	return []order.Product{
		{ID: "product-123", Name: "Health Potion", PriceCents: 9900, Active: true},
		{ID: "product-456", Name: "Mana Potion", PriceCents: 4950, Active: true},
		{ID: "sword-001", Name: "Iron Sword", PriceCents: 25000, Active: true},
		{ID: "retired-001", Name: "Old Item", PriceCents: 1000, Active: false},
	}
}

type orderKey struct {
	userID uuid.UUID
	key    string
}

type Store struct {
	mu       sync.Mutex
	products map[string]order.Product
	orders   map[orderKey]*order.Order
	// userExists emulates the orders.user_id FK; nil means "every user exists".
	userExists func(uuid.UUID) bool
}

func NewStore(products ...order.Product) *Store {
	s := &Store{products: map[string]order.Product{}, orders: map[orderKey]*order.Order{}}
	for _, p := range products {
		s.products[p.ID] = p
	}
	return s
}

// WithUserCheck makes InsertIfAbsent return order.ErrUserNotFound for
// unknown users, like the Postgres FK does.
func (s *Store) WithUserCheck(exists func(uuid.UUID) bool) *Store {
	s.userExists = exists
	return s
}

// UpsertProduct lets tests change prices / active flags.
func (s *Store) UpsertProduct(p order.Product) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.products[p.ID] = p
}

// Count returns the number of stored orders.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.orders)
}

func (s *Store) FindActive(_ context.Context, id string) (*order.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok || !p.Active {
		return nil, nil
	}
	return &p, nil
}

func (s *Store) ListActive(_ context.Context) ([]order.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]order.Product, 0, len(s.products))
	for _, p := range s.products {
		if p.Active {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) FindByKey(_ context.Context, userID uuid.UUID, key string) (*order.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.orders[orderKey{userID, key}]; ok {
		c := *o
		return &c, nil
	}
	return nil, nil
}

func (s *Store) InsertIfAbsent(_ context.Context, n order.NewOrder) (*order.Order, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := orderKey{n.UserID, n.IdempotencyKey}
	if o, ok := s.orders[k]; ok {
		c := *o
		return &c, false, nil
	}
	if s.userExists != nil && !s.userExists(n.UserID) {
		return nil, false, order.ErrUserNotFound
	}
	o := &order.Order{
		ID:             uuid.New(),
		UserID:         n.UserID,
		ProductID:      n.ProductID,
		Quantity:       n.Quantity,
		UnitPriceCents: n.UnitPriceCents,
		TotalCents:     n.TotalCents,
		IdempotencyKey: n.IdempotencyKey,
		CreatedAt:      time.Now().UTC(),
	}
	s.orders[k] = o
	c := *o
	return &c, true, nil
}

func (s *Store) ListByUser(_ context.Context, userID uuid.UUID, limit int) ([]order.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []order.Order{}
	for k, o := range s.orders {
		if k.userID == userID {
			out = append(out, *o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
