package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cocina/server-mvp/types"
)

// TokenService handles JWT token generation and validation
type TokenService struct {
	secret   string
	expiry   time.Duration
}

// NewTokenService creates a new token service
func NewTokenService(secret string) *TokenService {
	return &TokenService{
		secret: secret,
		expiry: 24 * time.Hour,
	}
}

// GenerateAccessToken creates a new access token for the user
func (s *TokenService) GenerateAccessToken(user *types.User) (string, error) {
	token := fmt.Sprintf("%s:%d", user.ID, time.Now().Unix())
	return simpleSign(token, s.secret), nil
}

// GenerateRefreshToken creates a new refresh token
func (s *TokenService) GenerateRefreshToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// ValidateAccessToken checks if the access token is valid
func (s *TokenService) ValidateAccessToken(token string) (*types.User, error) {
	parts := splitToken(token)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	userID := parts[0]
	timestampStr := parts[1]

	// Parse timestamp and check expiry
	var ts int64
	fmt.Sscanf(timestampStr, "%d", &ts)
	if time.Now().Unix()-ts > int64(s.expiry.Seconds()) {
		return nil, fmt.Errorf("token expired")
	}

	// In production, you would look up the user in the database
	// For MVP, we return a placeholder user
	return &types.User{ID: userID}, nil
}

func splitToken(token string) []string {
	result := make([]string, 2)
	parts := splitByColon(token)
	if len(parts) >= 2 {
		result[0] = parts[0]
		result[1] = parts[1]
	}
	return result
}

func splitByColon(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ':' {
			result = append(result, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func simpleSign(data, secret string) string {
	// Simple signing for MVP - in production use HMAC-SHA256
	return fmt.Sprintf("%s:%x", data, []byte(secret))
}
