package httpapi_test

import (
	"strings"
	"testing"
	"time"

	"order-backend/internal/auth"
	"order-backend/internal/config"
)

func newExpiredToken(t *testing.T, userID string) string {
	t.Helper()
	tok, _, err := auth.NewIssuer(testJWTSecret, "test-iss", -time.Minute).Issue(userID, "guest")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return tok
}

func TestAuthGuest(t *testing.T) {
	e := newEnv(t)
	r := e.do(t, "POST", "/api/auth/guest", `{"deviceId":"a1b2c3d4e5f6g7h8i9j0"}`)
	if r.status != 200 || r.body["accountType"] != "guest" || r.body["token"] == "" || r.body["expiresAt"] == "" {
		t.Fatalf("guest: %d %s", r.status, r.raw)
	}
	again := e.do(t, "POST", "/api/auth/guest", `{"deviceId":"a1b2c3d4e5f6g7h8i9j0"}`)
	if again.body["userId"] != r.body["userId"] {
		t.Fatal("same deviceId must return the same userId")
	}

	expectError(t, e.do(t, "POST", "/api/auth/guest", `{"deviceId":"short"}`), 400, "INVALID_DEVICE_ID")
	expectError(t, e.do(t, "POST", "/api/auth/guest", `{"deviceId":"a1b2c3d4e5f6g7h8i9j0","x":1}`), 400, "INVALID_JSON")
}

func TestAuthRegisterLoginMe(t *testing.T) {
	e := newEnv(t)

	reg := e.do(t, "POST", "/api/auth/register", `{"email":"  Player@Example.com ","password":"correct horse battery"}`)
	if reg.status != 201 || reg.body["accountType"] != "email" {
		t.Fatalf("register: %d %s", reg.status, reg.raw)
	}
	if strings.Contains(reg.raw, "password") || strings.Contains(reg.raw, "$2a$") {
		t.Fatalf("response must not leak password or hash: %s", reg.raw)
	}

	expectError(t, e.do(t, "POST", "/api/auth/register", `{"email":"player@example.com","password":"another-password"}`), 409, "EMAIL_TAKEN")
	expectError(t, e.do(t, "POST", "/api/auth/register", `{"email":"not-an-email","password":"password123"}`), 400, "INVALID_EMAIL")
	expectError(t, e.do(t, "POST", "/api/auth/register", `{"email":"x@example.com","password":"short"}`), 400, "WEAK_PASSWORD")

	login := e.do(t, "POST", "/api/auth/login", `{"email":"PLAYER@example.com","password":"correct horse battery"}`)
	if login.status != 200 || login.body["userId"] != reg.body["userId"] {
		t.Fatalf("login: %d %s", login.status, login.raw)
	}

	wrongPw := e.do(t, "POST", "/api/auth/login", `{"email":"player@example.com","password":"wrong-password"}`)
	noUser := e.do(t, "POST", "/api/auth/login", `{"email":"nobody@example.com","password":"correct horse battery"}`)
	expectError(t, wrongPw, 401, "INVALID_CREDENTIALS")
	expectError(t, noUser, 401, "INVALID_CREDENTIALS")
	if wrongPw.raw != noUser.raw {
		t.Fatalf("wrong password and unknown email must be indistinguishable:\n%s\n%s", wrongPw.raw, noUser.raw)
	}

	me := e.do(t, "GET", "/api/me", "", withToken(login.body["token"].(string)))
	if me.status != 200 || me.body["email"] != "player@example.com" || me.body["accountType"] != "email" {
		t.Fatalf("me: %d %s", me.status, me.raw)
	}
	expectError(t, e.do(t, "GET", "/api/me", ""), 401, "UNAUTHORIZED")
}

func TestAuthLinkKeepsUserAndOrders(t *testing.T) {
	e := newEnv(t)
	uid, guestTok := e.guestToken(t, "device-link-00000001")
	if r := e.do(t, "POST", "/api/orders", validOrder, withToken(guestTok), withKey("key-link-order1")); r.status != 201 {
		t.Fatalf("order: %d %s", r.status, r.raw)
	}

	link := e.do(t, "POST", "/api/auth/link", `{"email":"linked@example.com","password":"password123"}`, withToken(guestTok))
	if link.status != 200 || link.body["userId"] != uid || link.body["accountType"] != "email" {
		t.Fatalf("link: %d %s", link.status, link.raw)
	}
	expectError(t, e.do(t, "POST", "/api/auth/link", `{"email":"again@example.com","password":"password123"}`, withToken(guestTok)), 409, "ALREADY_LINKED")

	login := e.do(t, "POST", "/api/auth/login", `{"email":"linked@example.com","password":"password123"}`)
	orders := e.do(t, "GET", "/api/orders", "", withToken(login.body["token"].(string)))
	if len(orders.body["orders"].([]any)) != 1 {
		t.Fatalf("linked account must keep guest orders: %s", orders.raw)
	}
}

func TestAuthRateLimit(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.AuthRateLimitPerMin = 3 })
	var last response
	for range 4 {
		last = e.do(t, "POST", "/api/auth/login", `{"email":"x@example.com","password":"password123"}`)
	}
	expectError(t, last, 429, "RATE_LIMITED")
}
