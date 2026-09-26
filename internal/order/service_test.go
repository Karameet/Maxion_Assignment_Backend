package order_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"order-backend/internal/order"
	"order-backend/internal/order/memory"
)

const maxQty = 99

// spy counts every repository call so validation tests can assert that
// invalid input never reaches storage.
type spy struct {
	*memory.Store
	calls   atomic.Int64
	inserts []order.NewOrder
	mu      sync.Mutex
	// skipFastPath forces FindByKey to miss, so concurrent requests all race
	// into InsertIfAbsent (the path the UNIQUE constraint protects).
	skipFastPath bool
}

func (s *spy) FindActive(ctx context.Context, id string) (*order.Product, error) {
	s.calls.Add(1)
	return s.Store.FindActive(ctx, id)
}

func (s *spy) FindByKey(ctx context.Context, userID uuid.UUID, key string) (*order.Order, error) {
	s.calls.Add(1)
	if s.skipFastPath {
		return nil, nil
	}
	return s.Store.FindByKey(ctx, userID, key)
}

func (s *spy) InsertIfAbsent(ctx context.Context, n order.NewOrder) (*order.Order, bool, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.inserts = append(s.inserts, n)
	s.mu.Unlock()
	return s.Store.InsertIfAbsent(ctx, n)
}

func newSvc() (*order.Service, *spy) {
	sp := &spy{Store: memory.NewStore(memory.DefaultProducts()...)}
	return order.NewService(sp, sp, maxQty), sp
}

func input(user uuid.UUID, key, product string, qty int64) order.CreateOrderInput {
	return order.CreateOrderInput{UserID: user, IdempotencyKey: key, ProductID: product, Quantity: qty}
}

func TestCreateOrder_InvalidInputNeverTouchesRepo(t *testing.T) {
	u := uuid.New()
	cases := []struct {
		name string
		in   order.CreateOrderInput
		want error
	}{
		{"qty 0", input(u, "key-12345678", "product-123", 0), order.ErrInvalidQuantity},
		{"qty -1", input(u, "key-12345678", "product-123", -1), order.ErrInvalidQuantity},
		{"qty over max", input(u, "key-12345678", "product-123", maxQty+1), order.ErrInvalidQuantity},
		{"empty product", input(u, "key-12345678", "", 1), order.ErrInvalidProductID},
		{"long product", input(u, "key-12345678", strings.Repeat("p", 65), 1), order.ErrInvalidProductID},
		{"weird product", input(u, "key-12345678", "product 123;--", 1), order.ErrInvalidProductID},
		{"missing key", input(u, "", "product-123", 1), order.ErrMissingIdempotencyKey},
		{"short key", input(u, "short", "product-123", 1), order.ErrInvalidIdempotencyKey},
		{"key with space", input(u, "key 12345678", "product-123", 1), order.ErrInvalidIdempotencyKey},
		{"key too long", input(u, strings.Repeat("k", 129), "product-123", 1), order.ErrInvalidIdempotencyKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, sp := newSvc()
			_, err := svc.CreateOrder(context.Background(), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			if n := sp.calls.Load(); n != 0 {
				t.Fatalf("repo called %d times on invalid input", n)
			}
		})
	}
}

func TestCreateOrder_ValidComputesTotalServerSide(t *testing.T) {
	svc, sp := newSvc()
	u := uuid.New()
	res, err := svc.CreateOrder(context.Background(), input(u, "key-valid-0001", "product-123", 2))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if res.Replayed {
		t.Fatal("first create must not be a replay")
	}
	if res.Order.TotalCents != 19800 || res.Order.UnitPriceCents != 9900 {
		t.Fatalf("want total 19800 unit 9900, got %d / %d", res.Order.TotalCents, res.Order.UnitPriceCents)
	}
	if len(sp.inserts) != 1 {
		t.Fatalf("want 1 insert, got %d", len(sp.inserts))
	}
	got := sp.inserts[0]
	want := order.NewOrder{UserID: u, ProductID: "product-123", Quantity: 2, UnitPriceCents: 9900, TotalCents: 19800, IdempotencyKey: "key-valid-0001"}
	if got != want {
		t.Fatalf("repo got %+v, want %+v", got, want)
	}
}

func TestCreateOrder_ProductNotFoundOrInactive(t *testing.T) {
	for _, pid := range []string{"no-such-product", "retired-001"} {
		svc, sp := newSvc()
		_, err := svc.CreateOrder(context.Background(), input(uuid.New(), "key-notfound-01", pid, 1))
		if !errors.Is(err, order.ErrProductNotFound) {
			t.Fatalf("%s: want ErrProductNotFound, got %v", pid, err)
		}
		if len(sp.inserts) != 0 {
			t.Fatalf("%s: must not insert", pid)
		}
	}
}

func TestCreateOrder_ReplaySameBody(t *testing.T) {
	svc, sp := newSvc()
	u := uuid.New()
	ctx := context.Background()
	first, _ := svc.CreateOrder(ctx, input(u, "key-replay-0001", "product-123", 2))
	second, err := svc.CreateOrder(ctx, input(u, "key-replay-0001", "product-123", 2))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !second.Replayed || second.Order.ID != first.Order.ID {
		t.Fatalf("want replay of %v, got replayed=%v id=%v", first.Order.ID, second.Replayed, second.Order.ID)
	}
	if len(sp.inserts) != 1 || sp.Count() != 1 {
		t.Fatalf("replay must not insert again: inserts=%d stored=%d", len(sp.inserts), sp.Count())
	}
}

func TestCreateOrder_ReplayDifferentBody(t *testing.T) {
	svc, _ := newSvc()
	u := uuid.New()
	ctx := context.Background()
	_, _ = svc.CreateOrder(ctx, input(u, "key-reuse-00001", "product-123", 2))

	for _, in := range []order.CreateOrderInput{
		input(u, "key-reuse-00001", "product-123", 3),
		input(u, "key-reuse-00001", "product-456", 2),
	} {
		if _, err := svc.CreateOrder(ctx, in); !errors.Is(err, order.ErrIdempotencyKeyReused) {
			t.Fatalf("%+v: want ErrIdempotencyKeyReused, got %v", in, err)
		}
	}
}

func TestCreateOrder_SameKeyDifferentUsers(t *testing.T) {
	svc, sp := newSvc()
	ctx := context.Background()
	a, errA := svc.CreateOrder(ctx, input(uuid.New(), "shared-key-0001", "product-123", 1))
	b, errB := svc.CreateOrder(ctx, input(uuid.New(), "shared-key-0001", "product-123", 1))
	if errA != nil || errB != nil {
		t.Fatalf("errs: %v %v", errA, errB)
	}
	if a.Order.ID == b.Order.ID || b.Replayed || sp.Count() != 2 {
		t.Fatal("key must be scoped per user")
	}
}

func TestCreateOrder_ConcurrentSameKey(t *testing.T) {
	for _, skip := range []bool{false, true} {
		name := "with fast path"
		if skip {
			name = "all racing into insert"
		}
		t.Run(name, func(t *testing.T) {
			svc, sp := newSvc()
			sp.skipFastPath = skip
			u := uuid.New()
			const n = 50

			var wg sync.WaitGroup
			start := make(chan struct{})
			ids := make([]uuid.UUID, n)
			errs := make([]error, n)
			var created atomic.Int64
			for i := range n {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					res, err := svc.CreateOrder(context.Background(), input(u, "key-concurrent-1", "product-123", 2))
					errs[i] = err
					if err == nil {
						ids[i] = res.Order.ID
						if !res.Replayed {
							created.Add(1)
						}
					}
				}(i)
			}
			close(start)
			wg.Wait()

			for i, err := range errs {
				if err != nil {
					t.Fatalf("goroutine %d: %v", i, err)
				}
				if ids[i] != ids[0] {
					t.Fatalf("goroutine %d got id %v, want %v", i, ids[i], ids[0])
				}
			}
			if created.Load() != 1 || sp.Count() != 1 {
				t.Fatalf("want exactly 1 created order, created=%d stored=%d", created.Load(), sp.Count())
			}
		})
	}
}

func TestCreateOrder_ReplayAfterPriceChangeKeepsOriginalTotal(t *testing.T) {
	svc, sp := newSvc()
	u := uuid.New()
	ctx := context.Background()
	first, _ := svc.CreateOrder(ctx, input(u, "key-price-00001", "product-123", 2))

	sp.UpsertProduct(order.Product{ID: "product-123", Name: "Health Potion", PriceCents: 12345, Active: true})

	again, err := svc.CreateOrder(ctx, input(u, "key-price-00001", "product-123", 2))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if again.Order.TotalCents != first.Order.TotalCents || again.Order.TotalCents != 19800 {
		t.Fatalf("replay total changed: %d → %d", first.Order.TotalCents, again.Order.TotalCents)
	}
}

func TestMoneyMarshalJSON(t *testing.T) {
	cases := map[order.Money]string{
		19800: "198.00",
		0:     "0.00",
		5:     "0.05",
		4950:  "49.50",
		-150:  "-1.50",
	}
	for m, want := range cases {
		b, err := json.Marshal(m)
		if err != nil || string(b) != want {
			t.Fatalf("Money(%d) = %s, %v; want %s", int64(m), b, err, want)
		}
	}
}
