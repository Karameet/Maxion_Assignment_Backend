package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"

	"order-backend/internal/order"
)

// createOrderReq keeps both fields raw so the handler can tell "wrong JSON
// type" apart from "missing" without lenient coercion (e.g. "2" or 2.0).
type createOrderReq struct {
	ProductID json.RawMessage `json:"productId"`
	Quantity  json.RawMessage `json:"quantity"`
}

// orderResp is the assignment contract — do not add or rename fields.
type orderResp struct {
	ID        string      `json:"id"`
	ProductID string      `json:"productId"`
	Quantity  int         `json:"quantity"`
	Total     order.Money `json:"total"`
}

type orderListItem struct {
	ID        string      `json:"id"`
	ProductID string      `json:"productId"`
	Quantity  int         `json:"quantity"`
	UnitPrice order.Money `json:"unitPrice"`
	Total     order.Money `json:"total"`
	CreatedAt string      `json:"createdAt"`
}

func (s *Server) postOrder(c fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return Unauthorized(c, "invalid or expired token")
	}

	// Key is checked before the body so a missing key is reported even when
	// the body is also broken (documented validation order).
	key := c.Get(HeaderIdempotencyKey)
	if err := order.ValidateIdempotencyKey(key); err != nil {
		return mapOrderError(c, err)
	}

	var req createOrderReq
	if err := decodeStrict(c, &req); err != nil {
		return invalidJSON(c, err)
	}

	res, err := s.orderSvc.CreateOrder(c.Context(), order.CreateOrderInput{
		UserID:         userID,
		IdempotencyKey: key,
		ProductID:      rawString(req.ProductID),
		Quantity:       rawInteger(req.Quantity),
	})
	if err != nil {
		return mapOrderError(c, err)
	}

	c.Set(HeaderIdempotentReplayed, strconv.FormatBool(res.Replayed))
	o := res.Order
	return c.Status(fiber.StatusCreated).JSON(orderResp{
		ID:        o.ID.String(),
		ProductID: o.ProductID,
		Quantity:  o.Quantity,
		Total:     order.Money(o.TotalCents),
	})
}

func (s *Server) getOrders(c fiber.Ctx) error {
	userID, ok := userIDFromCtx(c)
	if !ok {
		return Unauthorized(c, "invalid or expired token")
	}
	orders, err := s.orderSvc.ListOrders(c.Context(), userID)
	if err != nil {
		return mapOrderError(c, err)
	}
	items := make([]orderListItem, 0, len(orders))
	for _, o := range orders {
		items = append(items, orderListItem{
			ID:        o.ID.String(),
			ProductID: o.ProductID,
			Quantity:  o.Quantity,
			UnitPrice: order.Money(o.UnitPriceCents),
			Total:     order.Money(o.TotalCents),
			CreatedAt: o.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return c.JSON(fiber.Map{"orders": items})
}

// rawString returns the value if raw is a JSON string, otherwise "" (which
// the service rejects as INVALID_PRODUCT_ID).
func rawString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// rawInteger accepts only a plain JSON integer literal. 2.5, 2.0, 1e2, "2",
// null and missing all become 0, which the service rejects as
// INVALID_QUANTITY.
func rawInteger(raw json.RawMessage) int64 {
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func mapOrderError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, order.ErrMissingIdempotencyKey):
		return WriteError(c, fiber.StatusBadRequest, CodeMissingIdempotencyKey, err.Error())
	case errors.Is(err, order.ErrInvalidIdempotencyKey):
		return WriteError(c, fiber.StatusBadRequest, CodeInvalidIdempotencyKey, err.Error())
	case errors.Is(err, order.ErrInvalidProductID):
		return WriteError(c, fiber.StatusBadRequest, CodeInvalidProductID, err.Error())
	case errors.Is(err, order.ErrInvalidQuantity):
		return WriteError(c, fiber.StatusBadRequest, CodeInvalidQuantity, err.Error())
	case errors.Is(err, order.ErrProductNotFound):
		return WriteError(c, fiber.StatusNotFound, CodeProductNotFound, err.Error())
	case errors.Is(err, order.ErrIdempotencyKeyReused):
		return WriteError(c, fiber.StatusUnprocessableEntity, CodeIdempotencyKeyReused, err.Error())
	case errors.Is(err, order.ErrUserNotFound):
		return Unauthorized(c, "account no longer exists")
	default:
		slog.Error("order", "err", err)
		return Internal(c)
	}
}
