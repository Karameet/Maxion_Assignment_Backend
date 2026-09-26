package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"order-backend/internal/order"
)

type ProductRepo struct {
	db *gorm.DB
}

func NewProductRepo(db *gorm.DB) *ProductRepo {
	return &ProductRepo{db: db}
}

func (r *ProductRepo) FindActive(ctx context.Context, id string) (*order.Product, error) {
	var p order.Product
	res := r.db.WithContext(ctx).Raw(`
		SELECT id, name, price_cents, active FROM products
		WHERE id = $1 AND active`, id).Scan(&p)
	if res.Error != nil {
		return nil, fmt.Errorf("find product: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return &p, nil
}

func (r *ProductRepo) ListActive(ctx context.Context) ([]order.Product, error) {
	var ps []order.Product
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, price_cents, active FROM products
		WHERE active ORDER BY id`).Scan(&ps).Error
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	return ps, nil
}
