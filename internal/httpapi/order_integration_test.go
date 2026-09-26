package httpapi_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"order-backend/internal/auth"
	"order-backend/internal/config"
	"order-backend/internal/httpapi"
	"order-backend/internal/order"
	pgstore "order-backend/internal/store/postgres"
	"order-backend/internal/user"
)

// Integration tests run only with TEST_DATABASE_URL (deliberately NOT
// DATABASE_URL): setup truncates users/orders, so point it at a dedicated,
// already-migrated database such as orders_test. See `make test-int`.
func setupPostgres(t *testing.T) (*testEnv, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Exec("TRUNCATE users CASCADE").Error; err != nil {
		t.Fatalf("truncate (did you run migrations on the test DB?): %v", err)
	}

	cfg := testConfig()
	cfg.OrderRepo = config.StorePostgres
	cfg.DatabaseURL = dsn
	srv := httpapi.New(cfg, httpapi.Deps{
		Users:  user.NewService(pgstore.NewUserRepo(db), auth.NewPasswordHasher(cfg.BcryptCost)),
		Orders: order.NewService(pgstore.NewProductRepo(db), pgstore.NewOrderRepo(db), cfg.MaxOrderQuantity),
		Issuer: auth.NewIssuer(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL),
		Health: func(ctx context.Context) error { return pgstore.Ping(ctx, db) },
	})
	return &testEnv{app: srv.App}, db
}

func countOrders(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT count(*) FROM orders").Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestPG_ConcurrentSameKeyCreatesOneOrder(t *testing.T) {
	e, db := setupPostgres(t)
	_, tok := e.guestToken(t, "device-pg-concurrent-01")
	key := uuid.NewString()

	const n = 50
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]response, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
		}(i)
	}
	close(start)
	wg.Wait()

	created := 0
	for i, r := range results {
		if r.status != 201 {
			t.Fatalf("request %d: %d %s", i, r.status, r.raw)
		}
		if r.body["id"] != results[0].body["id"] {
			t.Fatalf("request %d got id %v, want %v", i, r.body["id"], results[0].body["id"])
		}
		if r.header.Get("Idempotent-Replayed") == "false" {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("exactly one response should be the original, got %d", created)
	}
	if c := countOrders(t, db); c != 1 {
		t.Fatalf("SELECT count(*) FROM orders = %d, want 1", c)
	}
}

func TestPG_EndToEnd(t *testing.T) {
	e, db := setupPostgres(t)

	if r := e.do(t, "GET", "/healthz", ""); r.status != 200 {
		t.Fatalf("healthz: %d %s", r.status, r.raw)
	}

	reg := e.do(t, "POST", "/api/auth/register", `{"email":"E2E@Example.com","password":"password123"}`)
	if reg.status != 201 {
		t.Fatalf("register: %d %s", reg.status, reg.raw)
	}
	expectError(t, e.do(t, "POST", "/api/auth/register", `{"email":"e2e@example.com","password":"password123"}`), 409, "EMAIL_TAKEN")

	var stored string
	db.Raw("SELECT password_hash FROM users WHERE email = 'e2e@example.com'").Scan(&stored)
	if stored == "" || stored == "password123" || stored[:4] != "$2a$" {
		t.Fatalf("password must be stored as bcrypt hash, got %q", stored)
	}

	login := e.do(t, "POST", "/api/auth/login", `{"email":"e2e@example.com","password":"password123"}`)
	if login.status != 200 || login.body["userId"] != reg.body["userId"] {
		t.Fatalf("login: %d %s", login.status, login.raw)
	}
	expectError(t, e.do(t, "POST", "/api/auth/login", `{"email":"e2e@example.com","password":"nope-nope"}`), 401, "INVALID_CREDENTIALS")
	tok := login.body["token"].(string)

	key := uuid.NewString()
	first := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
	if first.status != 201 || first.header.Get("Idempotent-Replayed") != "false" {
		t.Fatalf("create: %d %s", first.status, first.raw)
	}

	// Price changes after the order; the replay must still return 198.00.
	db.Exec("UPDATE products SET price_cents = 1 WHERE id = 'product-123'")
	t.Cleanup(func() { db.Exec("UPDATE products SET price_cents = 9900 WHERE id = 'product-123'") })

	replay := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
	if replay.status != 201 || replay.raw != first.raw || replay.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: %d %s (first %s)", replay.status, replay.raw, first.raw)
	}

	expectError(t, e.do(t, "POST", "/api/orders", `{"productId":"product-123","quantity":5}`, withToken(tok), withKey(key)), 422, "IDEMPOTENCY_KEY_REUSED")
	expectError(t, e.do(t, "POST", "/api/orders", `{"productId":"retired-001","quantity":1}`, withToken(tok), withKey(uuid.NewString())), 404, "PRODUCT_NOT_FOUND")

	// Same key, different user → independent order.
	_, guestTok := e.guestToken(t, "device-pg-e2e-guest-01")
	other := e.do(t, "POST", "/api/orders", validOrder, withToken(guestTok), withKey(key))
	if other.status != 201 || other.body["id"] == first.body["id"] {
		t.Fatalf("key must be scoped per user: %d %s", other.status, other.raw)
	}

	list := e.do(t, "GET", "/api/orders", "", withToken(tok))
	if len(list.body["orders"].([]any)) != 1 {
		t.Fatalf("list: %s", list.raw)
	}
	if c := countOrders(t, db); c != 2 {
		t.Fatalf("want 2 orders total, got %d", c)
	}
}

func TestPG_DeletedUserTokenIs401(t *testing.T) {
	e, db := setupPostgres(t)
	uid, tok := e.guestToken(t, "device-pg-deleted-0001")
	db.Exec("DELETE FROM users WHERE id = $1", uid)
	expectError(t, e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(uuid.NewString())), 401, "UNAUTHORIZED")
	expectError(t, e.do(t, "GET", "/api/me", "", withToken(tok)), 401, "UNAUTHORIZED")
}

func TestPG_LinkGuestToEmail(t *testing.T) {
	e, _ := setupPostgres(t)
	uid, tok := e.guestToken(t, "device-pg-link-000001")
	e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(uuid.NewString()))

	link := e.do(t, "POST", "/api/auth/link", `{"email":"pglink@example.com","password":"password123"}`, withToken(tok))
	if link.status != 200 || link.body["userId"] != uid {
		t.Fatalf("link: %d %s", link.status, link.raw)
	}
	expectError(t, e.do(t, "POST", "/api/auth/link", `{"email":"x@example.com","password":"password123"}`, withToken(tok)), 409, "ALREADY_LINKED")

	_, tok2 := e.guestToken(t, "device-pg-link-000002")
	expectError(t, e.do(t, "POST", "/api/auth/link", `{"email":"pglink@example.com","password":"password123"}`, withToken(tok2)), 409, "EMAIL_TAKEN")

	me := e.do(t, "GET", "/api/me", "", withToken(link.body["token"].(string)))
	if me.body["accountType"] != "email" {
		t.Fatalf("me after link: %s", me.raw)
	}
}
