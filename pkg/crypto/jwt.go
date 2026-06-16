package crypto

import (
	"crypto/rsa"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenClaims represents the custom claims payload embedded inside the JWT Access Token.
type TokenClaims struct {
	TenantID string `json:"tid"`       // Multi-tenant isolation ID
	Role     string `json:"role"`      // RBAC authorization role
	DeviceID string `json:"device_id"` // Tracks multi-device sessions
	jwt.RegisteredClaims
}

// JWTManager handles signing access tokens with RS256 and verifying them against public keys.
type JWTManager struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// NewJWTManager instantiates a new JWTManager with pre-loaded RSA keys.
func NewJWTManager(privateKey *rsa.PrivateKey, publicKey *rsa.PublicKey) *JWTManager {
	return &JWTManager{
		privateKey: privateKey,
		publicKey:  publicKey,
	}
}

// GenerateAccessToken signs a new Access Token using RS256 with the private key.
func (m *JWTManager) GenerateAccessToken(userID, tenantID, role, deviceID string, duration time.Duration) (string, error) {
	claims := TokenClaims{
		TenantID: tenantID,
		Role:     role,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(m.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign access token: %w", err)
	}

	return tokenString, nil
}

// VerifyAccessToken verifies the token signature against the public key and decodes the claims.
func (m *JWTManager) VerifyAccessToken(tokenStr string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify standard signing algorithm
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected token signing algorithm: %v", token.Header["alg"])
		}
		return m.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parse and verify token: %w", err)
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims payload")
	}

	return claims, nil
}
