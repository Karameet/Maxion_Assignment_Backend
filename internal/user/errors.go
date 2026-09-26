package user

import "errors"

var (
	ErrInvalidDeviceID    = errors.New("deviceId must be 16-128 characters")
	ErrInvalidEmail       = errors.New("email is not a valid address")
	ErrWeakPassword       = errors.New("password must be 8-72 bytes")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAlreadyLinked      = errors.New("account already has an email")
	ErrNotFound           = errors.New("user not found")
)
