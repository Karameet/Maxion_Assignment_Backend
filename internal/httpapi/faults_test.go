package httpapi_test

import (
	"testing"
	"time"

	"order-backend/internal/config"
)

func devFaults(c *config.Config) {
	c.AppEnv = config.EnvDev
	c.EnableFaultInjection = true
}

func TestFaultInjection_DisabledInProd(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.AppEnv = config.EnvProd
		c.EnableFaultInjection = true // Load() rejects this; router must still ignore it
	})
	_, tok := e.guestToken(t, "device-fault-prod-001")
	r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-fault-prod1"), withHeader("X-Debug-Fault", "status=500"))
	if r.status != 201 {
		t.Fatalf("fault header must be ignored in prod, got %d %s", r.status, r.raw)
	}
}

func TestFaultInjection_DisabledByDefaultInDev(t *testing.T) {
	e := newEnv(t)
	_, tok := e.guestToken(t, "device-fault-off-0001")
	r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-fault-off01"), withHeader("X-Debug-Fault", "status=500"))
	if r.status != 201 {
		t.Fatalf("fault header must be ignored unless enabled, got %d", r.status)
	}
}

func TestFaultInjection_Status(t *testing.T) {
	e := newEnv(t, devFaults)
	_, tok := e.guestToken(t, "device-fault-status01")
	for status, code := range map[string]struct {
		n    int
		code string
	}{
		"status=500": {500, "INTERNAL"},
		"status=503": {503, "SERVICE_UNAVAILABLE"},
		"status=403": {403, "FORBIDDEN"},
		"status=401": {401, "UNAUTHORIZED"},
	} {
		r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-fault-stat1"), withHeader("X-Debug-Fault", status))
		expectError(t, r, code.n, code.code)
	}
	if e.store.Count() != 0 {
		t.Fatal("status faults must short-circuit before the handler")
	}
	expectError(t, e.do(t, "GET", "/healthz", "", withHeader("X-Debug-Fault", "explode")), 400, "BAD_REQUEST")
	expectError(t, e.do(t, "GET", "/healthz", "", withHeader("X-Debug-Fault", "status=418")), 400, "BAD_REQUEST")
	expectError(t, e.do(t, "GET", "/healthz", "", withHeader("X-Debug-Fault", "delay=1h")), 400, "BAD_REQUEST")
}

// The demo scenario: the server commits the order but the response is lost;
// the client retries with the same key and gets the original order back.
func TestFaultInjection_DropAfterCommitThenRetry(t *testing.T) {
	e := newEnv(t, devFaults)
	_, tok := e.guestToken(t, "device-fault-drop-001")
	key := "key-fault-drop-01"

	lost := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key), withHeader("X-Debug-Fault", "drop-after-commit"))
	expectError(t, lost, 500, "INTERNAL")
	if lost.header.Get("Idempotent-Replayed") != "" {
		t.Fatal("dropped response must not carry Idempotent-Replayed")
	}
	if e.store.Count() != 1 {
		t.Fatalf("order must be committed despite the 500, stored=%d", e.store.Count())
	}

	retry := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey(key))
	if retry.status != 201 || retry.header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("retry: %d replayed=%q %s", retry.status, retry.header.Get("Idempotent-Replayed"), retry.raw)
	}
	if e.store.Count() != 1 {
		t.Fatalf("retry must not create a second order, stored=%d", e.store.Count())
	}
}

func TestFaultInjection_DelayStillProcesses(t *testing.T) {
	e := newEnv(t, devFaults)
	_, tok := e.guestToken(t, "device-fault-delay-01")
	start := time.Now()
	r := e.do(t, "POST", "/api/orders", validOrder, withToken(tok), withKey("key-fault-delay1"), withHeader("X-Debug-Fault", "delay=200ms"))
	if r.status != 201 || time.Since(start) < 200*time.Millisecond {
		t.Fatalf("delay: %d after %v", r.status, time.Since(start))
	}
}
