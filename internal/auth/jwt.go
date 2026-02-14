package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims is the payload inside every JWT token.
//
// When a user logs in, we create a token containing these fields.
// On every subsequent request, the middleware reads the token back
// and extracts these claims — this is how the server knows WHO
// is making the request without hitting the database every time.
//
// Why embed jwt.RegisteredClaims?
//   - It gives us standard JWT fields for free: ExpiresAt, IssuedAt, Issuer.
//   - Libraries and tooling (jwt.io debugger) recognize these standard fields.
//   - We add our custom fields (UserID, TenantID, Email) on top.
type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	TenantID uuid.UUID `json:"tenant_id"`
	Email    string    `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed JWT for a given user.
//
// Parameters:
//   - userID, tenantID, email: who this token represents.
//   - secret: the HMAC key to sign with (from config.JWTSecret).
//   - ttl: how long until the token expires (e.g., 24 * time.Hour).
//
// Returns the signed token string (e.g., "eyJhbGciOi...").
//
// Why HS256 (HMAC-SHA256)?
//   - Simple: one shared secret, no public/private key pair needed.
//   - Fast: symmetric crypto is faster than RSA/ECDSA.
//   - Fine for a single-service backend. If we had multiple services
//     that need to VERIFY but not ISSUE tokens, we'd switch to RS256
//     (asymmetric) so only the auth service has the private key.
func GenerateToken(userID, tenantID uuid.UUID, email, secret string, ttl time.Duration) (string, error) {
	now := time.Now()

	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
		RegisteredClaims: jwt.RegisteredClaims{
			// ExpiresAt: when this token becomes invalid.
			// After this time, the middleware will reject it.
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			// IssuedAt: when the token was created. Useful for debugging
			// ("was this token issued before the password was changed?").
			IssuedAt: jwt.NewNumericDate(now),
			// Issuer: identifies who created the token. Helps if you
			// ever have multiple services issuing tokens.
			Issuer: "echostream",
		},
	}

	// jwt.NewWithClaims creates an unsigned token with our claims.
	// SignedString signs it with our secret and returns the final string.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return signed, nil
}

// ParseToken validates a JWT string and extracts the claims.
//
// It verifies:
//   1. The signature matches our secret (not tampered with).
//   2. The token hasn't expired (ExpiresAt is in the future).
//   3. The signing method is HMAC (prevents algorithm-switching attacks).
//
// Returns the Claims if valid, or an error describing what's wrong.
func ParseToken(tokenString, secret string) (*Claims, error) {
	// jwt.ParseWithClaims does three things:
	//   1. Base64-decodes the token
	//   2. Unmarshals the JSON payload into our Claims struct
	//   3. Verifies the signature using the key from our callback
	token, err := jwt.ParseWithClaims(tokenString, &Claims{},
		func(token *jwt.Token) (any, error) {
			// This callback is called BEFORE signature verification.
			// We check that the signing method is HMAC — if someone sends
			// a token signed with "none" or RSA, we reject it immediately.
			// This prevents the classic JWT "algorithm confusion" attack.
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(secret), nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	// Type-assert the claims back to our custom Claims type.
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
