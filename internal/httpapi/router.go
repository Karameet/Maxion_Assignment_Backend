package httpapi

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"order-backend/internal/auth"
	"order-backend/internal/config"
	"order-backend/internal/order"
	"order-backend/internal/user"
)

const healthPingTimeout = 3 * time.Second

// HealthFunc checks backing storage. nil means "nothing to check" (memory mode).
type HealthFunc func(ctx context.Context) error

type Deps struct {
	Health HealthFunc
	Users  *user.Service
	Orders *order.Service
	Issuer *auth.Issuer
}

type Server struct {
	App      *fiber.App
	cfg      *config.Config
	health   HealthFunc
	userSvc  *user.Service
	orderSvc *order.Service
	issuer   *auth.Issuer
}

func New(cfg *config.Config, d Deps) *Server {
	app := fiber.New(fiber.Config{
		AppName:      "order-api",
		BodyLimit:    cfg.MaxBodyBytes,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorHandler: errorHandler,
	})

	allowHeaders := []string{fiber.HeaderContentType, fiber.HeaderAuthorization, HeaderIdempotencyKey}
	if cfg.FaultInjectionActive() {
		allowHeaders = append(allowHeaders, HeaderDebugFault)
	}

	// Chain: recover → RequestLogger → cors → (dev) FaultInjection → routes.
	app.Use(recover.New())
	app.Use(RequestLogger())
	app.Use(cors.New(cors.Config{
		AllowOrigins:  cfg.CORSAllowedOrigins,
		AllowHeaders:  allowHeaders,
		AllowMethods:  []string{fiber.MethodGet, fiber.MethodPost, fiber.MethodOptions},
		ExposeHeaders: []string{HeaderIdempotentReplayed},
	}))
	if cfg.FaultInjectionActive() {
		app.Use(FaultInjection())
	}

	s := &Server{
		App:      app,
		cfg:      cfg,
		health:   d.Health,
		userSvc:  d.Users,
		orderSvc: d.Orders,
		issuer:   d.Issuer,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.App.Get("/healthz", s.healthz)

	api := s.App.Group("/api")
	bearer := BearerAuth(s.issuer)

	// NOTE: Fiber runs route handlers in argument order — middleware first.
	authLimit := s.authRateLimiter()
	api.Post("/auth/guest", s.postAuthGuest)
	api.Post("/auth/register", authLimit, s.postAuthRegister)
	api.Post("/auth/login", authLimit, s.postAuthLogin)
	api.Post("/auth/link", bearer, s.postAuthLink)

	api.Get("/me", bearer, s.getMe)
	api.Get("/products", s.getProducts)

	if s.cfg.OrdersRequireEmailAccount {
		api.Post("/orders", bearer, RequireEmailAccount(), s.postOrder)
	} else {
		api.Post("/orders", bearer, s.postOrder)
	}
	api.Get("/orders", bearer, s.getOrders)
}

// authRateLimiter returns one limiter shared by login and register (budget
// per client IP), or a pass-through when AUTH_RATE_LIMIT_PER_MIN=0.
func (s *Server) authRateLimiter() fiber.Handler {
	if s.cfg.AuthRateLimitPerMin <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	return limiter.New(limiter.Config{
		Max:        s.cfg.AuthRateLimitPerMin,
		Expiration: time.Minute,
		LimitReached: func(c fiber.Ctx) error {
			return WriteError(c, fiber.StatusTooManyRequests, CodeRateLimited, "too many requests, try again later")
		},
	})
}

func (s *Server) healthz(c fiber.Ctx) error {
	if s.health != nil {
		ctx, cancel := context.WithTimeout(c.Context(), healthPingTimeout)
		defer cancel()
		if err := s.health(ctx); err != nil {
			return WriteError(c, fiber.StatusServiceUnavailable, CodeServiceUnavailable, "database unavailable")
		}
	}
	return c.JSON(fiber.Map{"status": "ok"})
}
