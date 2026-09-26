package auth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHasher_HashIsSaltedAndVerifiable(t *testing.T) {
	h := NewPasswordHasher(bcrypt.MinCost)
	const pw = "correct horse battery"

	a, err := h.Hash(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := h.Hash(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if a == pw || !strings.HasPrefix(a, "$2a$") {
		t.Fatalf("hash does not look like bcrypt: %q", a)
	}
	if a == b {
		t.Fatal("two hashes of the same password must differ (random salt)")
	}
	if !h.Compare(a, pw) || !h.Compare(b, pw) {
		t.Fatal("Compare must accept both hashes")
	}
	if h.Compare(a, pw+"x") {
		t.Fatal("Compare must reject wrong password")
	}
}
