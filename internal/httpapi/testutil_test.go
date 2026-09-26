package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/crypto/bcrypt"

	"order-backend/internal/auth"
	"order-backend/internal/config"
	"order-backend/internal/httpapi"
	"order-backend/internal/order"
	ordermem "order-backend/internal/order/memory"
	"order-backend/internal/user"
	usermem "order-backend/internal/user/memory"
)

const testJWTSecret = "handler-test-secret-at-least-32-chars"

type testEnv struct {
	app    *fiber.App
	users  *usermem.Repo
	store  *ordermem.Store
	issuer *auth.Issuer
}

func testConfig() *config.Config {
	return &config.Config{
		Port:               "0",
		AppEnv:             config.EnvDev,
		JWTSecret:          testJWTSecret,
		JWTIssuer:          "test-iss",
		JWTTTL:             time.Hour,
		BcryptCost:         bcrypt.MinCost,
		MaxBodyBytes:       16384,
		MaxOrderQuantity:   99,
		OrderRepo:          config.StoreMemory,
		CORSAllowedOrigins: []string{"*"},
	}
}

func newEnv(t *testing.T, mutate ...func(*config.Config)) *testEnv {
	t.Helper()
	cfg := testConfig()
	for _, m := range mutate {
		m(cfg)
	}
	users := usermem.NewRepo()
	store := ordermem.NewStore(ordermem.DefaultProducts()...).WithUserCheck(users.Exists)
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL)
	srv := httpapi.New(cfg, httpapi.Deps{
		Users:  user.NewService(users, auth.NewPasswordHasher(cfg.BcryptCost)),
		Orders: order.NewService(store, store, cfg.MaxOrderQuantity),
		Issuer: issuer,
	})
	return &testEnv{app: srv.App, users: users, store: store, issuer: issuer}
}

type response struct {
	status int
	header http.Header
	raw    string
	body   map[string]any
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }
func withToken(tok string) reqOpt   { return withHeader("Authorization", "Bearer "+tok) }
func withKey(key string) reqOpt     { return withHeader("Idempotency-Key", key) }

func (e *testEnv) do(t *testing.T, method, path, body string, opts ...reqOpt) response {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := e.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	r := response{status: resp.StatusCode, header: resp.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &r.body)
	return r
}

// expectError asserts status + code and that the body is the standard
// {"error":{"code","message"}} envelope.
func expectError(t *testing.T, r response, status int, code string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("want status %d, got %d body=%s", status, r.status, r.raw)
	}
	errObj, ok := r.body["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error envelope: %s", r.raw)
	}
	if errObj["code"] != code {
		t.Fatalf("want code %s, got %v (body=%s)", code, errObj["code"], r.raw)
	}
	if msg, _ := errObj["message"].(string); msg == "" {
		t.Fatalf("error message must be non-empty: %s", r.raw)
	}
	if len(r.body) != 1 {
		t.Fatalf("error body must contain only \"error\": %s", r.raw)
	}
}

func (e *testEnv) guestToken(t *testing.T, deviceID string) (userID, token string) {
	t.Helper()
	r := e.do(t, "POST", "/api/auth/guest", fmt.Sprintf(`{"deviceId":%q}`, deviceID))
	if r.status != 200 {
		t.Fatalf("guest login: %d %s", r.status, r.raw)
	}
	return r.body["userId"].(string), r.body["token"].(string)
}
