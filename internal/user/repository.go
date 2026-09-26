package user

import (
	"context"

	"github.com/google/uuid"
)

// Repository persists users. It holds no business rules. Methods that store a
// password only accept an already-computed bcrypt hash — there is no way to
// pass plaintext down to storage.
type Repository interface {
	UpsertByDeviceID(ctx context.Context, deviceID string) (*User, error)
	// CreateEmailUser returns ErrEmailTaken on unique violation.
	CreateEmailUser(ctx context.Context, email, passwordHash string) (*User, error)
	// FindByEmail returns ErrNotFound when no row matches.
	FindByEmail(ctx context.Context, email string) (*User, error)
	// FindByID returns ErrNotFound when no row matches.
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
	// LinkEmail attaches email+hash to a row that has no email yet.
	// Returns ErrNotFound, ErrAlreadyLinked or ErrEmailTaken.
	LinkEmail(ctx context.Context, id uuid.UUID, email, passwordHash string) (*User, error)
}
