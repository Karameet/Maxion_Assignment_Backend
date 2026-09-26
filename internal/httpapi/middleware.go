package httpapi

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"order-backend/internal/auth"
	"order-backend/internal/user"
)

const localsClaims = "claims"

// BearerAuth validates the Authorization: Bearer <token> header and stores
// the claims in c.Locals on success. Missing/invalid/expired tokens all
// produce a uniform 401 UNAUTHORIZED. JWT is stateless: no DB access here.
func BearerAuth(iss *auth.Issuer) fiber.Handler {
	return func(c fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			return Unauthorized(c, "missing bearer token")
		}
		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if token == "" {
			return Unauthorized(c, "missing bearer token")
		}
		claims, err := iss.Parse(token)
		if err != nil {
			return Unauthorized(c, "invalid or expired token")
		}
		if _, err := uuid.Parse(claims.UserID); err != nil {
			return Unauthorized(c, "invalid or expired token")
		}
		c.Locals(localsClaims, claims)
		return c.Next()
	}
}

// RequireEmailAccount rejects guest tokens with 403 FORBIDDEN (§6.5, only
// mounted when ORDERS_REQUIRE_EMAIL_ACCOUNT=true).
func RequireEmailAccount() fiber.Handler {
	return func(c fiber.Ctx) error {
		if claimsFromCtx(c).AccountType != string(user.AccountEmail) {
			return WriteError(c, fiber.StatusForbidden, CodeForbidden, "an email account is required for this action")
		}
		return c.Next()
	}
}

func claimsFromCtx(c fiber.Ctx) auth.Claims {
	v, _ := c.Locals(localsClaims).(auth.Claims)
	return v
}

// userIDFromCtx returns the caller's user id. BearerAuth already verified it
// parses, so a failure here means the route is missing the middleware.
func userIDFromCtx(c fiber.Ctx) (uuid.UUID, bool) {
	id, err := uuid.Parse(claimsFromCtx(c).UserID)
	return id, err == nil
}
