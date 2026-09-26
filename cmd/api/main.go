package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"order-backend/internal/auth"
	"order-backend/internal/config"
	"order-backend/internal/httpapi"
	"order-backend/internal/order"
	ordermem "order-backend/internal/order/memory"
	pgstore "order-backend/internal/store/postgres"
	"order-backend/internal/user"
	usermem "order-backend/internal/user/memory"
)

const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hasher := auth.NewPasswordHasher(cfg.BcryptCost)
	issuer := auth.NewIssuer(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTTTL)
	deps := httpapi.Deps{Issuer: issuer}

	switch cfg.OrderRepo {
	case config.StoreMemory:
		slog.Warn("ORDER_REPO=memory: all data is in-process and lost on restart")
		users := usermem.NewRepo()
		store := ordermem.NewStore(ordermem.DefaultProducts()...).WithUserCheck(users.Exists)
		deps.Users = user.NewService(users, hasher)
		deps.Orders = order.NewService(store, store, cfg.MaxOrderQuantity)

	default:
		db, err := pgstore.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			slog.Error("open db", "err", err)
			os.Exit(1)
		}
		defer func() {
			if sqlDB, err := db.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}()
		deps.Users = user.NewService(pgstore.NewUserRepo(db), hasher)
		deps.Orders = order.NewService(pgstore.NewProductRepo(db), pgstore.NewOrderRepo(db), cfg.MaxOrderQuantity)
		deps.Health = func(ctx context.Context) error { return pgstore.Ping(ctx, db) }
	}

	if cfg.FaultInjectionActive() {
		slog.Warn("fault injection ENABLED via X-Debug-Fault header (dev only)")
	}

	srv := httpapi.New(cfg, deps)

	go func() {
		addr := ":" + cfg.Port
		slog.Info("api listening", "addr", addr, "env", cfg.AppEnv, "store", cfg.OrderRepo)
		if err := srv.App.Listen(addr); err != nil {
			slog.Error("listen", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	if err := srv.App.ShutdownWithTimeout(shutdownTimeout); err != nil {
		slog.Error("shutdown", "err", err)
	}
	slog.Info("stopped")
}
