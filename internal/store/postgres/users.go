package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"order-backend/internal/user"
)

const userColumns = "id, device_id, email, password_hash, created_at, last_login_at"

type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

// UpsertByDeviceID is the canonical guest-login upsert.
// New device → inserts a row. Existing device → updates last_login_at.
func (r *UserRepo) UpsertByDeviceID(ctx context.Context, deviceID string) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO users (device_id) VALUES ($1)
		ON CONFLICT (device_id) DO UPDATE SET last_login_at = now()
		RETURNING `+userColumns, deviceID).Scan(&u).Error
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) CreateEmailUser(ctx context.Context, email, passwordHash string) (*user.User, error) {
	var u user.User
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO users (email, password_hash) VALUES ($1, $2)
		RETURNING `+userColumns, email, passwordHash).Scan(&u).Error
	if pgErrorIs(err, pgUniqueViolation, "users_email_key") {
		return nil, user.ErrEmailTaken
	}
	if err != nil {
		return nil, fmt.Errorf("create email user: %w", err)
	}
	return &u, nil
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	return r.findOne(ctx, "SELECT "+userColumns+" FROM users WHERE email = $1", email)
}

func (r *UserRepo) FindByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return r.findOne(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id)
}

func (r *UserRepo) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	err := r.db.WithContext(ctx).Exec(`UPDATE users SET last_login_at = now() WHERE id = $1`, id).Error
	if err != nil {
		return fmt.Errorf("touch last login: %w", err)
	}
	return nil
}

func (r *UserRepo) LinkEmail(ctx context.Context, id uuid.UUID, email, passwordHash string) (*user.User, error) {
	var u user.User
	res := r.db.WithContext(ctx).Raw(`
		UPDATE users SET email = $2, password_hash = $3
		WHERE id = $1 AND email IS NULL
		RETURNING `+userColumns, id, email, passwordHash).Scan(&u)
	if pgErrorIs(res.Error, pgUniqueViolation, "users_email_key") {
		return nil, user.ErrEmailTaken
	}
	if res.Error != nil {
		return nil, fmt.Errorf("link email: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		// Either the user is gone or it already has an email.
		if _, err := r.FindByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, user.ErrAlreadyLinked
	}
	return &u, nil
}

func (r *UserRepo) findOne(ctx context.Context, query string, arg any) (*user.User, error) {
	var u user.User
	res := r.db.WithContext(ctx).Raw(query, arg).Scan(&u)
	if res.Error != nil {
		return nil, fmt.Errorf("find user: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, user.ErrNotFound
	}
	return &u, nil
}
