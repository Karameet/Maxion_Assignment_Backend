// Package memory is a thread-safe in-memory user.Repository used by unit
// tests and by ORDER_REPO=memory demo mode.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"order-backend/internal/user"
)

type Repo struct {
	mu      sync.Mutex
	byID    map[uuid.UUID]*user.User
	byEmail map[string]uuid.UUID
	byDev   map[string]uuid.UUID
}

func NewRepo() *Repo {
	return &Repo{
		byID:    map[uuid.UUID]*user.User{},
		byEmail: map[string]uuid.UUID{},
		byDev:   map[string]uuid.UUID{},
	}
}

func (r *Repo) UpsertByDeviceID(_ context.Context, deviceID string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	if id, ok := r.byDev[deviceID]; ok {
		u := r.byID[id]
		u.LastLoginAt = now
		return clone(u), nil
	}
	d := deviceID
	u := &user.User{ID: uuid.New(), DeviceID: &d, CreatedAt: now, LastLoginAt: now}
	r.byID[u.ID] = u
	r.byDev[deviceID] = u.ID
	return clone(u), nil
}

func (r *Repo) CreateEmailUser(_ context.Context, email, passwordHash string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byEmail[email]; ok {
		return nil, user.ErrEmailTaken
	}
	now := time.Now().UTC()
	e, h := email, passwordHash
	u := &user.User{ID: uuid.New(), Email: &e, PasswordHash: &h, CreatedAt: now, LastLoginAt: now}
	r.byID[u.ID] = u
	r.byEmail[email] = u.ID
	return clone(u), nil
}

func (r *Repo) FindByEmail(_ context.Context, email string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byEmail[email]
	if !ok {
		return nil, user.ErrNotFound
	}
	return clone(r.byID[id]), nil
}

func (r *Repo) FindByID(_ context.Context, id uuid.UUID) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	return clone(u), nil
}

func (r *Repo) TouchLastLogin(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id]; ok {
		u.LastLoginAt = time.Now().UTC()
	}
	return nil
}

func (r *Repo) LinkEmail(_ context.Context, id uuid.UUID, email, passwordHash string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, user.ErrNotFound
	}
	if u.Email != nil {
		return nil, user.ErrAlreadyLinked
	}
	if _, taken := r.byEmail[email]; taken {
		return nil, user.ErrEmailTaken
	}
	e, h := email, passwordHash
	u.Email, u.PasswordHash = &e, &h
	r.byEmail[email] = id
	return clone(u), nil
}

// Delete removes a user (tests use it to simulate "token outlives account").
func (r *Repo) Delete(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return
	}
	if u.Email != nil {
		delete(r.byEmail, *u.Email)
	}
	if u.DeviceID != nil {
		delete(r.byDev, *u.DeviceID)
	}
	delete(r.byID, id)
}

// Exists lets the in-memory order store emulate the orders.user_id FK.
func (r *Repo) Exists(id uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.byID[id]
	return ok
}

func clone(u *user.User) *user.User {
	c := *u
	return &c
}
