package order

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Money is an amount in integer cents. It never passes through float64, and
// marshals as a JSON number with exactly two decimals (19800 → 198.00).
type Money int64

func (m Money) MarshalJSON() ([]byte, error) {
	neg := m < 0
	if neg {
		m = -m
	}
	s := fmt.Sprintf("%d.%02d", int64(m)/100, int64(m)%100)
	if neg {
		s = "-" + s
	}
	return []byte(s), nil
}

type Product struct {
	ID         string `gorm:"column:id"`
	Name       string `gorm:"column:name"`
	PriceCents int64  `gorm:"column:price_cents"`
	Active     bool   `gorm:"column:active"`
}

type Order struct {
	ID             uuid.UUID `gorm:"column:id"`
	UserID         uuid.UUID `gorm:"column:user_id"`
	ProductID      string    `gorm:"column:product_id"`
	Quantity       int       `gorm:"column:quantity"`
	UnitPriceCents int64     `gorm:"column:unit_price_cents"` // price snapshot at order time
	TotalCents     int64     `gorm:"column:total_cents"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// NewOrder is what the service asks the repository to insert.
type NewOrder struct {
	UserID         uuid.UUID
	ProductID      string
	Quantity       int
	UnitPriceCents int64
	TotalCents     int64
	IdempotencyKey string
}
