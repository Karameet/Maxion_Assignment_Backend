package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	EnvDev  = "dev"
	EnvProd = "prod"

	StorePostgres = "postgres"
	StoreMemory   = "memory"

	minJWTSecretLen = 32
)

type Config struct {
	Port                      string
	AppEnv                    string
	DatabaseURL               string
	JWTSecret                 string
	JWTIssuer                 string
	JWTTTL                    time.Duration
	BcryptCost                int
	MaxBodyBytes              int
	MaxOrderQuantity          int
	OrderRepo                 string // "postgres" | "memory" — memory mode keeps users in memory too
	EnableFaultInjection      bool
	OrdersRequireEmailAccount bool
	AuthRateLimitPerMin       int // 0 disables the limiter on login/register
	CORSAllowedOrigins        []string
}

// FaultInjectionActive reports whether the dev-only X-Debug-Fault middleware
// should be mounted. Load() already rejects prod+enabled; this is the second
// guard used by the router.
func (c *Config) FaultInjectionActive() bool {
	return c.AppEnv == EnvDev && c.EnableFaultInjection
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	c := &Config{
		Port:      getEnv("PORT", "8080"),
		AppEnv:    getEnv("APP_ENV", EnvDev),
		JWTIssuer: getEnv("JWT_ISSUER", "order-api"),
		OrderRepo: getEnv("ORDER_REPO", StorePostgres),
	}

	if c.AppEnv != EnvDev && c.AppEnv != EnvProd {
		return nil, fmt.Errorf("invalid APP_ENV %q (want dev|prod)", c.AppEnv)
	}
	if c.OrderRepo != StorePostgres && c.OrderRepo != StoreMemory {
		return nil, fmt.Errorf("invalid ORDER_REPO %q (want postgres|memory)", c.OrderRepo)
	}

	c.DatabaseURL = os.Getenv("DATABASE_URL")
	if c.OrderRepo == StorePostgres && c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required when ORDER_REPO=postgres")
	}

	c.JWTSecret = os.Getenv("JWT_SECRET")
	if len(c.JWTSecret) < minJWTSecretLen {
		return nil, fmt.Errorf("JWT_SECRET is required and must be at least %d chars", minJWTSecretLen)
	}

	var err error
	if c.JWTTTL, err = getDuration("JWT_TTL", "168h"); err != nil {
		return nil, err
	}
	if c.BcryptCost, err = getInt("BCRYPT_COST", 12, 4, 31); err != nil {
		return nil, err
	}
	if c.MaxBodyBytes, err = getInt("MAX_BODY_BYTES", 16384, 256, 1<<20); err != nil {
		return nil, err
	}
	if c.MaxOrderQuantity, err = getInt("MAX_ORDER_QUANTITY", 99, 1, 10000); err != nil {
		return nil, err
	}
	if c.AuthRateLimitPerMin, err = getInt("AUTH_RATE_LIMIT_PER_MIN", 10, 0, 100000); err != nil {
		return nil, err
	}
	if c.EnableFaultInjection, err = getBool("ENABLE_FAULT_INJECTION", false); err != nil {
		return nil, err
	}
	if c.OrdersRequireEmailAccount, err = getBool("ORDERS_REQUIRE_EMAIL_ACCOUNT", false); err != nil {
		return nil, err
	}

	if c.AppEnv == EnvProd && c.EnableFaultInjection {
		return nil, errors.New("ENABLE_FAULT_INJECTION must not be true when APP_ENV=prod")
	}

	c.CORSAllowedOrigins = splitAndTrim(getEnv("CORS_ALLOWED_ORIGINS", "*"))

	return c, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getDuration(key, fallback string) (time.Duration, error) {
	s := getEnv(key, fallback)
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid %s %q", key, s)
	}
	return d, nil
}

func getInt(key string, fallback, min, max int) (int, error) {
	s := getEnv(key, strconv.Itoa(fallback))
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("invalid %s %q (want integer %d..%d)", key, s, min, max)
	}
	return n, nil
}

func getBool(key string, fallback bool) (bool, error) {
	s := getEnv(key, strconv.FormatBool(fallback))
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q", key, s)
	}
	return b, nil
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
