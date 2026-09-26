package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("token expired")
)

// Claims is what a verified token tells the HTTP layer about the caller.
type Claims struct {
	UserID      string
	AccountType string // "guest" | "email"
}

type tokenClaims struct {
	Acct string `json:"acct,omitempty"`
	jwt.RegisteredClaims
}

type Issuer struct {
	secret []byte
	iss    string
	ttl    time.Duration
}

func NewIssuer(secret, iss string, ttl time.Duration) *Issuer {
	return &Issuer{secret: []byte(secret), iss: iss, ttl: ttl}
}

// Issue mints an HS256 JWT with sub=userID, acct=accountType, iss=configured
// issuer, iat/exp set. Returns the signed token and its expiry (UTC).
func (i *Issuer) Issue(userID, accountType string) (string, time.Time, error) {
	now := time.Now().UTC()
	exp := now.Add(i.ttl)
	claims := tokenClaims{
		Acct: accountType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    i.iss,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(i.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, exp, nil
}

// Parse validates the token and returns its claims.
// Pins algorithm to HS256 to defeat alg=none / algorithm confusion attacks.
func (i *Issuer) Parse(tokenStr string) (Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithIssuer(i.iss),
		jwt.WithExpirationRequired(),
	)
	claims := &tokenClaims{}
	_, err := parser.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		return i.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, ErrExpiredToken
		}
		return Claims{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		return Claims{}, ErrInvalidToken
	}
	return Claims{UserID: claims.Subject, AccountType: claims.Acct}, nil
}
