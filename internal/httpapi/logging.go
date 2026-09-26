package httpapi

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
)

// RequestLogger logs one line per completed request. Deliberately captures
// only method, path, status, duration and client IP — never headers, body,
// passwords, tokens or Idempotency-Key values.
func RequestLogger() fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		if err := c.Next(); err != nil {
			// Render the error now so the logged status matches what the
			// client receives (e.g. 404 for unknown routes, not 200).
			if hErr := c.App().ErrorHandler(c, err); hErr != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}

		status := c.Response().StatusCode()
		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
		}

		switch {
		case status >= 500:
			slog.Error("request", attrs...)
		case status >= 400:
			slog.Warn("request", attrs...)
		default:
			slog.Info("request", attrs...)
		}
		return nil
	}
}
