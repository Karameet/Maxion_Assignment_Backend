package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "unit-test-secret-at-least-32-chars!!"

func TestIssueParseRoundtrip(t *testing.T) {
	iss := NewIssuer(testSecret, "test-iss", time.Hour)
	tok, exp, err := iss.Issue("user-123", "email")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if time.Until(exp) < 55*time.Minute {
		t.Fatalf("expiry too close: %v", exp)
	}
	claims, err := iss.Parse(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != "user-123" || claims.AccountType != "email" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestParseRejectsAlgNone(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   "user-123",
		Issuer:    "test-iss",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("craft alg=none token: %v", err)
	}
	iss := NewIssuer(testSecret, "test-iss", time.Hour)
	if _, err := iss.Parse(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseRejectsRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	claims := jwt.RegisteredClaims{
		Subject:   "user-123",
		Issuer:    "test-iss",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign rs256: %v", err)
	}
	iss := NewIssuer(testSecret, "test-iss", time.Hour)
	if _, err := iss.Parse(signed); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseRejectsExpiredToken(t *testing.T) {
	iss := NewIssuer(testSecret, "test-iss", -time.Minute) // already expired
	tok, _, err := iss.Issue("user-123", "guest")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := iss.Parse(tok); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expected ErrExpiredToken, got %v", err)
	}
}

func TestParseRejectsWrongSignature(t *testing.T) {
	issA := NewIssuer(testSecret, "test-iss", time.Hour)
	issB := NewIssuer("different-secret-also-32-chars-long!", "test-iss", time.Hour)
	tok, _, _ := issA.Issue("user-123", "guest")
	if _, err := issB.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseRejectsWrongIssuer(t *testing.T) {
	issA := NewIssuer(testSecret, "issuer-a", time.Hour)
	issB := NewIssuer(testSecret, "issuer-b", time.Hour)
	tok, _, _ := issA.Issue("user-123", "guest")
	if _, err := issB.Parse(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	iss := NewIssuer(testSecret, "test-iss", time.Hour)
	if _, err := iss.Parse("not-a-jwt"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
