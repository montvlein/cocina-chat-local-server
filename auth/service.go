package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"github.com/cocina/server-mvp/types"
)

// AuthService handles user authentication operations
type AuthService struct {
	db      *sql.DB
	token   *TokenService
}

// NewAuthService creates a new auth service
func NewAuthService(db *sql.DB, tokenService *TokenService) *AuthService {
	return &AuthService{db: db, token: tokenService}
}

// Register creates a new user account
func (s *AuthService) Register(email, username, password string) (*types.AuthResponse, error) {
	// Check if email or username already exists
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ? OR username = ?", email, username).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("database query failed: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("email or username already exists")
	}

	// Hash password
	passwordHash := hashPassword(password)

	// Generate unique user ID using crypto/rand
	userID, err := generateUniqueID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate user ID: %w", err)
	}

	// Insert user
	_, err = s.db.Exec(
		"INSERT INTO users (id, email, username, password_hash, display_name) VALUES (?, ?, ?, ?, ?)",
		userID, email, username, passwordHash, username,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	// Create tokens
	accessToken, err := s.token.GenerateAccessToken(&types.User{ID: userID, Email: email, Username: username})
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := s.token.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Store session in database
	expiresAt := time.Now().Add(24 * time.Hour)
	sessionID, _ := generateUniqueID()
	_, err = s.db.Exec(
		"INSERT INTO sessions (id, user_id, token, expires_at) VALUES (?, ?, ?, ?)",
		sessionID, userID, refreshToken, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	user := &types.User{
		ID:        userID,
		Email:     email,
		Username:  username,
		CreatedAt: time.Now(),
	}

	return &types.AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(24 * time.Hour / time.Second),
	}, nil
}

// Login authenticates a user and returns tokens
func (s *AuthService) Login(email, password string) (*types.AuthResponse, error) {
	// Find user by email
	var userID, username, passwordHash string
	err := s.db.QueryRow("SELECT id, username, password_hash FROM users WHERE email = ?", email).Scan(&userID, &username, &passwordHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, fmt.Errorf("database query failed: %w", err)
	}

	// Verify password
	if !verifyPassword(password, passwordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Create tokens
	user := &types.User{ID: userID, Email: email, Username: username}
	accessToken, err := s.token.GenerateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := s.token.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Store session
	expiresAt := time.Now().Add(24 * time.Hour)
	sessionID, _ := generateUniqueID()
	_, err = s.db.Exec(
		"INSERT INTO sessions (id, user_id, token, expires_at) VALUES (?, ?, ?, ?)",
		sessionID, userID, refreshToken, expiresAt.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &types.AuthResponse{
		User:         user,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(24 * time.Hour / time.Second),
	}, nil
}

// Logout invalidates the current session
func (s *AuthService) Logout(refreshToken string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token = ?", refreshToken)
	return err
}

// GetUserByID retrieves a user by their ID
func (s *AuthService) GetUserByID(id string) (*types.User, error) {
	var email, username string
	var createdAt time.Time
	err := s.db.QueryRow(
		"SELECT id, email, username, created_at FROM users WHERE id = ?", id,
	).Scan(&id, &email, &username, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &types.User{
		ID:        id,
		Email:     email,
		Username:  username,
		CreatedAt: createdAt,
	}, nil
}

// Helper functions for password hashing
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return fmt.Sprintf("$sha256$%x", hash)
}

func verifyPassword(password, hash string) bool {
	computedHash := fmt.Sprintf("$sha256$%x", sha256.Sum256([]byte(password)))
	return computedHash == hash
}

// generateUniqueID generates a cryptographically secure unique ID
func generateUniqueID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return fmt.Sprintf("user_%x", bytes), nil
}
