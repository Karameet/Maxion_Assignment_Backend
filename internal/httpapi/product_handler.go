package httpapi

import (
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"order-backend/internal/order"
)

type productResp struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Price order.Money `json:"price"`
}

func (s *Server) getProducts(c fiber.Ctx) error {
	products, err := s.orderSvc.ListProducts(c.Context())
	if err != nil {
		slog.Error("list products", "err", err)
		return Internal(c)
	}
	items := make([]productResp, 0, len(products))
	for _, p := range products {
		items = append(items, productResp{ID: p.ID, Name: p.Name, Price: order.Money(p.PriceCents)})
	}
	return c.JSON(fiber.Map{"products": items})
}
