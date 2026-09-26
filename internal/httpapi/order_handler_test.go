package httpapi_test

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"order-backend/internal/config"
)

const validOrder = `{"productId":"product-123","quantity":2}`

func TestPostOrder_NoBearer(t *testing.T) {
	e := newEnv(t)
	for _, opts := range [][]reqOpt{
		{withKey("key-no-bearer-1")},
		{withKey("key-no-bearer-1"), withHeader("Authorization", "Basic abc")},
		{withKey("key-no-bearer-1"), withToken("not-a-jwt")},
	} {
		expectError(t, e.do(t, "POST", "/api/orders", validOrder, opts...), 401, "UNAUTHORIZED")
	}
	if e.store.Count() != 0 {
		t.Fatal("no order may be created without auth")
	}
}

func TestPostOrder_ExpiredToken(t *testing.T) {
	e := newEnv(t)
	uid, _ := e.guestToken(t, "device-expired-000001")
	expired := newExpiredToken(t, uid)
	expectError(t, e.do(t, "POST", "/api/orders", validOrder, withToken(expired), withKey("key-expired-01")), 401, "UNAUTHORIZED")
}

func TestPostOrder_IdempotencyKeyValidation(t *testing.T) {
	e := newEnv(t)
	_, tok := e.guestToken(t, "device-keyval-0000001")

	expectError(t, e.do(t, "POST", "/api/orders", validOrder, withToken(tok)), 400, "MISSING_IDEMPOTENCY_KEY")
	// Key is validated before the body: broken body + missing key → key error.
	expectError(t, e.do(t, "POST", "/api/orders", `{broken`, withToken(tok)), 400, "MISSING_IDEMPOTENCY_KEY")

	for _, bad := range []string{"short", "has space 12345", strings.Repeat("k", 129), "key/with/slash"} {
		expectError(t, e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(bad)), 400, "INVALID_IDEMPOTENCY_KEY")
	}
}

func TestPostOrder_BodyValidation(t *testing.T) {
	e := newEnv(t)
	_, tok := e.guestToken(t, "device-bodyval-000001")

	cases := []struct {
		name, body, code string
	}{
		{"client-sent total", `{"productId":"product-123","quantity":2,"total":1}`, "INVALID_JSON"},
		{"client-sent price", `{"productId":"product-123","quantity":2,"price":0.01}`, "INVALID_JSON"},
		{"malformed", `{"productId":`, "INVALID_JSON"},
		{"array", `[1,2]`, "INVALID_JSON"},
		{"trailing data", validOrder + `{}`, "INVALID_JSON"},
		{"empty", ``, "INVALID_JSON"},

		{"qty float", `{"productId":"product-123","quantity":2.5}`, "INVALID_QUANTITY"},
		{"qty 2.0", `{"productId":"product-123","quantity":2.0}`, "INVALID_QUANTITY"},
		{"qty exp", `{"productId":"product-123","quantity":1e2}`, "INVALID_QUANTITY"},
		{"qty string", `{"productId":"product-123","quantity":"2"}`, "INVALID_QUANTITY"},
		{"qty zero", `{"productId":"product-123","quantity":0}`, "INVALID_QUANTITY"},
		{"qty negative", `{"productId":"product-123","quantity":-1}`, "INVALID_QUANTITY"},
		{"qty null", `{"productId":"product-123","quantity":null}`, "INVALID_QUANTITY"},
		{"qty missing", `{"productId":"product-123"}`, "INVALID_QUANTITY"},
		{"qty over max", `{"productId":"product-123","quantity":100}`, "INVALID_QUANTITY"},
		{"qty huge", `{"productId":"product-123","quantity":99999999999999999999999}`, "INVALID_QUANTITY"},

		{"product number", `{"productId":123,"quantity":1}`, "INVALID_PRODUCT_ID"},
		{"product missing", `{"quantity":1}`, "INVALID_PRODUCT_ID"},
		{"product empty", `{"productId":"","quantity":1}`, "INVALID_PRODUCT_ID"},
		{"product injection", `{"productId":"x' OR '1'='1","quantity":1}`, "INVALID_PRODUCT_ID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := e.do(t, "POST", "/api/orders", tc.body, withToken(tok), withKey("key-"+uuid.NewString()))
			if tc.body == "" {
				// do() skips Content-Type for empty bodies; send it explicitly.
				r = e.do(t, "POST", "/api/orders", "", withToken(tok), withKey("key-"+uuid.NewString()),
					withHeader("Content-Type", "application/json"))
			}
			expectError(t, r, 400, tc.code)
		})
	}

	r := e.do(t, "POST", "/api/orders", `{"productId":"product-123","quantity":2,"total":1}`, withToken(tok), withKey("key-unknown-f1"))
	if !strings.Contains(r.raw, "total") {
		t.Fatalf("unknown-field message should name the field: %s", r.raw)
	}

	wrongCT := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-wrong-ct-1"), withHeader("Content-Type", "text/plain"))
	expectError(t, wrongCT, 400, "INVALID_JSON")

	if e.store.Count() != 0 {
		t.Fatalf("invalid requests created %d orders", e.store.Count())
	}
}

func TestPostOrder_CreateAndReplay(t *testing.T) {
	e := newEnv(t)
	_, tok := e.guestToken(t, "device-create-0000001")
	key := uuid.NewString()

	first := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
	if first.status != 201 {
		t.Fatalf("want 201, got %d %s", first.status, first.raw)
	}
	if !strings.Contains(first.raw, `"total":198.00`) {
		t.Fatalf("total must serialize as 198.00 exactly: %s", first.raw)
	}
	if first.header.Get("Idempotent-Replayed") != "false" {
		t.Fatalf("first response Idempotent-Replayed = %q", first.header.Get("Idempotent-Replayed"))
	}
	if first.body["productId"] != "product-123" || first.body["quantity"] != float64(2) {
		t.Fatalf("unexpected body: %s", first.raw)
	}
	if len(first.body) != 4 {
		t.Fatalf("contract body has exactly id, productId, quantity, total: %s", first.raw)
	}

	second := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
	if second.status != 201 || second.body["id"] != first.body["id"] {
		t.Fatalf("replay must return 201 with same id: %d %s", second.status, second.raw)
	}
	if second.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay Idempotent-Replayed = %q", second.header.Get("Idempotent-Replayed"))
	}
	if second.raw != first.raw {
		t.Fatalf("replay body differs:\n%s\n%s", first.raw, second.raw)
	}
	if e.store.Count() != 1 {
		t.Fatalf("want 1 stored order, got %d", e.store.Count())
	}

	reuse := e.do(t, "POST", "/api/orders", `{"productId":"product-123","quantity":3}`, withToken(tok), withKey(key))
	expectError(t, reuse, 422, "IDEMPOTENCY_KEY_REUSED")
}

func TestPostOrder_ProductNotFound(t *testing.T) {
	e := newEnv(t)
	_, tok := e.guestToken(t, "device-notfound-00001")
	for _, pid := range []string{"no-such-product", "retired-001"} {
		r := e.do(t, "POST", "/api/orders", `{"productId":"`+pid+`","quantity":1}`, withToken(tok), withKey("key-"+uuid.NewString()))
		expectError(t, r, 404, "PRODUCT_NOT_FOUND")
	}
}

func TestPostOrder_DeletedUserIs401(t *testing.T) {
	e := newEnv(t)
	uid, tok := e.guestToken(t, "device-deleted-000001")
	e.users.Delete(uuid.MustParse(uid))
	r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-deleted-01"))
	expectError(t, r, 401, "UNAUTHORIZED")
}

// The body limit is enforced by fasthttp while reading the request, which
// app.Test() bypasses, so this test goes through a real TCP listener.
func TestPostOrder_PayloadTooLarge(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.MaxBodyBytes = 1024 })
	_, tok := e.guestToken(t, "device-toolarge-00001")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = e.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = e.app.Shutdown() })

	big := `{"productId":"` + strings.Repeat("a", 4096) + `","quantity":1}`
	req, _ := http.NewRequest("POST", "http://"+ln.Addr().String()+"/api/orders", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Idempotency-Key", "key-too-large1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	r := response{status: resp.StatusCode, header: resp.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &r.body)
	expectError(t, r, 413, "PAYLOAD_TOO_LARGE")
}

func TestPostOrder_RequireEmailAccount(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.OrdersRequireEmailAccount = true })
	_, guestTok := e.guestToken(t, "device-guest-forbid01")
	expectError(t, e.do(t, "POST", "/api/orders", validOrder, withToken(guestTok), withKey("key-forbid-001")), 403, "FORBIDDEN")

	reg := e.do(t, "POST", "/api/auth/register", `{"email":"buyer@example.com","password":"password123"}`)
	emailTok := reg.body["token"].(string)
	if r := e.do(t, "POST", "/api/orders", validOrder, withToken(emailTok), withKey("key-forbid-002")); r.status != 201 {
		t.Fatalf("email account should be allowed: %d %s", r.status, r.raw)
	}
}

func TestGetOrdersAndProducts(t *testing.T) {
	e := newEnv(t)
	_, tokA := e.guestToken(t, "device-list-a-0000001")
	_, tokB := e.guestToken(t, "device-list-b-0000001")
	e.do(t, "POST", "/api/orders", validOrder, withToken(tokA), withKey("key-list-000001"))

	a := e.do(t, "GET", "/api/orders", "", withToken(tokA))
	b := e.do(t, "GET", "/api/orders", "", withToken(tokB))
	if a.status != 200 || len(a.body["orders"].([]any)) != 1 {
		t.Fatalf("A should see 1 order: %s", a.raw)
	}
	if b.status != 200 || len(b.body["orders"].([]any)) != 0 {
		t.Fatalf("B must not see A's orders: %s", b.raw)
	}
	expectError(t, e.do(t, "GET", "/api/orders", ""), 401, "UNAUTHORIZED")

	p := e.do(t, "GET", "/api/products", "")
	if p.status != 200 || strings.Contains(p.raw, "retired-001") || !strings.Contains(p.raw, `"price":99.00`) {
		t.Fatalf("products: %d %s", p.status, p.raw)
	}
}

func TestUnknownRouteUsesEnvelope(t *testing.T) {
	e := newEnv(t)
	expectError(t, e.do(t, "GET", "/api/nope", ""), 404, "NOT_FOUND")
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	if r := e.do(t, "GET", "/healthz", ""); r.status != 200 || r.body["status"] != "ok" {
		t.Fatalf("healthz: %d %s", r.status, r.raw)
	}
}

func TestCORSAllowsIdempotencyHeaders(t *testing.T) {
	e := newEnv(t)
	pre := e.do(t, "OPTIONS", "/api/orders", "",
		withHeader("Origin", "http://game.example"),
		withHeader("Access-Control-Request-Method", "POST"),
		withHeader("Access-Control-Request-Headers", "authorization,content-type,idempotency-key"))
	if allow := strings.ToLower(pre.header.Get("Access-Control-Allow-Headers")); !strings.Contains(allow, "idempotency-key") {
		t.Fatalf("preflight must allow Idempotency-Key, got %q (status %d)", allow, pre.status)
	}

	_, tok := e.guestToken(t, "device-cors-000000001")
	r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-cors-000001"), withHeader("Origin", "http://game.example"))
	if exp := r.header.Get("Access-Control-Expose-Headers"); !strings.Contains(exp, "Idempotent-Replayed") {
		t.Fatalf("must expose Idempotent-Replayed, got %q", exp)
	}
}
