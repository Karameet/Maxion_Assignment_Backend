package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// PasswordHasher wraps bcrypt. Cost comes from BCRYPT_COST (prod 12, tests
// use bcrypt.MinCost for speed).
type PasswordHasher struct {
	cost int
}

func NewPasswordHasher(cost int) PasswordHasher {
	return PasswordHasher{cost: cost}
}

// Hash returns a salted bcrypt hash ("$2a$<cost>$..."). Salt is random per
// call, so hashing the same password twice gives different strings.
func (h PasswordHasher) Hash(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// Compare checks plain against a stored hash in constant time. Never compare
// by re-hashing: the salt differs every time.
func (h PasswordHasher) Compare(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
