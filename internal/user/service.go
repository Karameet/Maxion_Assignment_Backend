package user

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"sync"

	"github.com/google/uuid"
)

const (
	minDeviceIDLen = 16
	maxDeviceIDLen = 128
	maxEmailLen    = 254
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt silently truncates beyond 72 bytes — reject instead
)

// PasswordHasher is satisfied by auth.PasswordHasher.
type PasswordHasher interface {
	Hash(plain string) (string, error)
	Compare(hash, plain string) bool
}

type Service struct {
	repo   Repository
	hasher PasswordHasher

	dummyOnce sync.Once
	dummyHash string
}

func NewService(repo Repository, hasher PasswordHasher) *Service {
	return &Service{repo: repo, hasher: hasher}
}

func (s *Service) LoginGuest(ctx context.Context, deviceID string) (*User, error) {
	if n := len(deviceID); n < minDeviceIDLen || n > maxDeviceIDLen {
		return nil, ErrInvalidDeviceID
	}
	return s.repo.UpsertByDeviceID(ctx, deviceID)
}

func (s *Service) Register(ctx context.Context, email, password string) (*User, error) {
	email, hash, err := s.prepareCredentials(email, password)
	if err != nil {
		return nil, err
	}
	return s.repo.CreateEmailUser(ctx, email, hash)
}

// LoginEmail returns ErrInvalidCredentials for both "no such email" and
// "wrong password" so callers cannot enumerate accounts.
func (s *Service) LoginEmail(ctx context.Context, email, password string) (*User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	u, err := s.repo.FindByEmail(ctx, email)
	if errors.Is(err, ErrNotFound) {
		// Burn the same bcrypt time as a real check so response latency does
		// not reveal whether the email exists.
		s.hasher.Compare(s.dummy(), password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if u.PasswordHash == nil || !s.hasher.Compare(*u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	if err := s.repo.TouchLastLogin(ctx, u.ID); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*User, error) {
	return s.repo.FindByID(ctx, id)
}

// LinkEmail upgrades a guest account in place: userId and orders are kept.
func (s *Service) LinkEmail(ctx context.Context, id uuid.UUID, email, password string) (*User, error) {
	email, hash, err := s.prepareCredentials(email, password)
	if err != nil {
		return nil, err
	}
	return s.repo.LinkEmail(ctx, id, email, hash)
}

// prepareCredentials validates input and hashes the password. Plaintext never
// travels past this function.
func (s *Service) prepareCredentials(email, password string) (string, string, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return "", "", err
	}
	if err := ValidatePassword(password); err != nil {
		return "", "", err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return "", "", err
	}
	return email, hash, nil
}

func (s *Service) dummy() string {
	s.dummyOnce.Do(func() {
		h, err := s.hasher.Hash("timing-equalizer-not-a-real-password")
		if err == nil {
			s.dummyHash = h
		}
	})
	return s.dummyHash
}

// NormalizeEmail trims and lowercases, then requires a bare RFC 5322 address
// (rejects display-name forms like "Name <a@b.com>").
func NormalizeEmail(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > maxEmailLen {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e {
		return "", ErrInvalidEmail
	}
	return e, nil
}

func ValidatePassword(pw string) error {
	if n := len(pw); n < minPasswordLen || n > maxPasswordLen {
		return ErrWeakPassword
	}
	return nil
}
