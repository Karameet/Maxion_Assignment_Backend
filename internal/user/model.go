package user

import (
	"time"

	"github.com/google/uuid"
)

type AccountType string

const (
	AccountGuest AccountType = "guest"
	AccountEmail AccountType = "email"
)

// User is one row of the users table. Guest rows have DeviceID set; email rows
// have Email + PasswordHash set (a linked guest has all three).
type User struct {
	ID           uuid.UUID `gorm:"column:id"`
	DeviceID     *string   `gorm:"column:device_id"`
	Email        *string   `gorm:"column:email"`
	PasswordHash *string   `gorm:"column:password_hash" json:"-"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	LastLoginAt  time.Time `gorm:"column:last_login_at"`
}

func (User) TableName() string { return "users" }

func (u *User) AccountType() AccountType {
	if u.Email != nil {
		return AccountEmail
	}
	return AccountGuest
}
