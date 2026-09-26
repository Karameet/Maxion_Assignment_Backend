package config

import (
	"strings"
	"testing"
)

const goodSecret = "config-test-secret-at-least-32-chars"

// setEnv clears every variable Load reads, then applies overrides, so a
// developer's local .env or shell cannot leak into the test.
func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for _, k := range []string{
		"PORT", "APP_ENV", "DATABASE_URL", "JWT_SECRET", "JWT_ISSUER", "JWT_TTL",
		"BCRYPT_COST", "MAX_BODY_BYTES", "MAX_ORDER_QUANTITY", "ORDER_REPO",
		"ENABLE_FAULT_INJECTION", "ORDERS_REQUIRE_EMAIL_ACCOUNT",
		"AUTH_RATE_LIMIT_PER_MIN", "CORS_ALLOWED_ORIGINS",
	} {
		t.Setenv(k, "")
	}
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret})
	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.AppEnv != EnvDev || c.OrderRepo != StorePostgres || c.MaxOrderQuantity != 99 ||
		c.BcryptCost != 12 || c.MaxBodyBytes != 16384 || c.JWTIssuer != "order-api" || c.FaultInjectionActive() {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoad_FailFast(t *testing.T) {
	cases := map[string]map[string]string{
		"prod with fault injection": {"APP_ENV": "prod", "ENABLE_FAULT_INJECTION": "true", "DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret},
		"short jwt secret":          {"DATABASE_URL": "postgres://x", "JWT_SECRET": "too-short"},
		"missing database url":      {"JWT_SECRET": goodSecret},
		"bad app env":               {"APP_ENV": "staging", "DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret},
		"bad order repo":            {"ORDER_REPO": "redis", "DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret},
		"zero max quantity":         {"MAX_ORDER_QUANTITY": "0", "DATABASE_URL": "postgres://x", "JWT_SECRET": goodSecret},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			setEnv(t, env)
			if _, err := Load(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoad_MemoryModeNeedsNoDatabase(t *testing.T) {
	setEnv(t, map[string]string{"ORDER_REPO": "memory", "JWT_SECRET": goodSecret, "ENABLE_FAULT_INJECTION": "true"})
	c, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !c.FaultInjectionActive() {
		t.Fatal("dev + ENABLE_FAULT_INJECTION should activate fault injection")
	}
	if strings.Join(c.CORSAllowedOrigins, ",") != "*" {
		t.Fatalf("cors default: %v", c.CORSAllowedOrigins)
	}
}
