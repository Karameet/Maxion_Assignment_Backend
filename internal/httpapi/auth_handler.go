package httpapi

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"order-backend/internal/user"
)

type guestLoginReq struct {
	DeviceID string `json:"deviceId"`
}

type credentialsReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResp struct {
	UserID      string `json:"userId"`
	Token       string `json:"token"`
	ExpiresAt   string `json:"expiresAt"`
	AccountType string `json:"accountType"`
}

type meResp struct {
	UserID      string  `json:"userId"`
	AccountType string  `json:"accountType"`
	Email       *string `json:"email"`
	CreatedAt   string  `json:"createdAt"`
}

func (s *Server) postAuthGuest(c fiber.Ctx) error {
	var req guestLoginReq
	if err := decodeStrict(c, &req); err != nil {
		return invalidJSON(c, err)
	}
	u, err := s.userSvc.LoginGuest(c.Context(), req.DeviceID)
	if err != nil {
		return s.mapUserError(c, err, "login guest")
	}
	return s.respondWithToken(c, fiber.StatusOK, u)
}

func (s *Server) postAuthRegister(c fiber.Ctx) error {
	var req credentialsReq
	if err := decodeStrict(c, &req); err != nil {
		return invalidJSON(c, err)
	}
	u, err := s.userSvc.Register(c.Context(), req.Email, req.Password)
	if err != nil {
		return s.mapUserError(c, err, "register")
	}
	return s.respondWithToken(c, fiber.StatusCreated, u)
}

func (s *Server) postAuthLogin(c fiber.Ctx) error {
	var req credentialsReq
	if err := decodeStrict(c, &req); err != nil {
		return invalidJSON(c, err)
	}
	u, err := s.userSvc.LoginEmail(c.Context(), req.Email, req.Password)
	if err != nil {
		return s.mapUserError(c, err, "login email")
	}
	return s.respondWithToken(c, fiber.StatusOK, u)
}

// postAuthLink upgrades the calling guest account to an email account,
// keeping the same userId (and therefore its orders). Returns a fresh token
// carrying acct=email.
func (s *Server) postAuthLink(c fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return Unauthorized(c, "invalid or expired token")
	}
	var req credentialsReq
	if err := decodeStrict(c, &req); err != nil {
		return invalidJSON(c, err)
	}
	u, err := s.userSvc.LinkEmail(c.Context(), userID, req.Email, req.Password)
	if err != nil {
		return s.mapUserError(c, err, "link email")
	}
	return s.respondWithToken(c, fiber.StatusOK, u)
}

func (s *Server) getMe(c fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return Unauthorized(c, "invalid or expired token")
	}
	u, err := s.userSvc.Get(c.Context(), userID)
	if err != nil {
		return s.mapUserError(c, err, "get me")
	}
	return c.JSON(meResp{
		UserID:      u.ID.String(),
		AccountType: string(u.AccountType()),
		Email:       u.Email,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) respondWithToken(c fiber.Ctx, status int, u *user.User) error {
	acct := string(u.AccountType())
	token, exp, err := s.issuer.Issue(u.ID.String(), acct)
	if err != nil {
		slog.Error("issue jwt", "err", err)
		return Internal(c)
	}
	return c.Status(status).JSON(authResp{
		UserID:      u.ID.String(),
		Token:       token,
		ExpiresAt:   exp.UTC().Format(time.RFC3339),
		AccountType: acct,
	})
}

func (s *Server) mapUserError(c fiber.Ctx, err error, op string) error {
	switch {
	case errors.Is(err, user.ErrInvalidDeviceID):
		return WriteError(c, fiber.StatusBadRequest, CodeInvalidDeviceID, err.Error())
	case errors.Is(err, user.ErrInvalidEmail):
		return WriteError(c, fiber.StatusBadRequest, CodeInvalidEmail, err.Error())
	case errors.Is(err, user.ErrWeakPassword):
		return WriteError(c, fiber.StatusBadRequest, CodeWeakPassword, err.Error())
	case errors.Is(err, user.ErrInvalidCredentials):
		return WriteError(c, fiber.StatusUnauthorized, CodeInvalidCredentials, err.Error())
	case errors.Is(err, user.ErrNotFound):
		// Token is valid but the account is gone.
		return Unauthorized(c, "account no longer exists")
	case errors.Is(err, user.ErrEmailTaken):
		return WriteError(c, fiber.StatusConflict, CodeEmailTaken, err.Error())
	case errors.Is(err, user.ErrAlreadyLinked):
		return WriteError(c, fiber.StatusConflict, CodeAlreadyLinked, err.Error())
	default:
		slog.Error(op, "err", err)
		return Internal(c)
	}
}
