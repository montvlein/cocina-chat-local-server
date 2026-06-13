package auth

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cocina/server-mvp/config"
	"github.com/cocina/server-mvp/ids"
	"github.com/cocina/server-mvp/types"
)

const identityPasswordSentinel = "$identity$"

type OrgProvisioner interface {
	SyncIdentityMemberships(localUserID, bearerToken string) error
}

// AuthService handles user authentication operations
type AuthService struct {
	db       *sql.DB
	local    *TokenService
	identity *IdentityValidator
	mode     string
	orgSvc   OrgProvisioner
}

// NewAuthService creates a new auth service
func NewAuthService(db *sql.DB, cfg *config.Config) *AuthService {
	svc := &AuthService{
		db:    db,
		local: NewTokenService(cfg.JWTSecret),
		mode:  cfg.AuthMode,
	}
	if cfg.IdentityEnabled() {
		svc.identity = NewIdentityValidator(cfg.IdentityJWKS, cfg.IdentityIssuer)
	}
	return svc
}

func (s *AuthService) SetOrgProvisioner(orgSvc OrgProvisioner) {
	s.orgSvc = orgSvc
}

// ValidateAccessToken validates a bearer token and returns the local user.
func (s *AuthService) ValidateAccessToken(token string) (*types.User, error) {
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}

	if s.identity != nil && looksLikeJWT(token) {
		user, err := s.validateIdentityToken(token)
		if err == nil {
			return user, nil
		}
		if s.mode == "identity" {
			return nil, err
		}
	}

	if s.mode == "identity" {
		return nil, fmt.Errorf("invalid token")
	}

	localUser, err := s.local.ValidateAccessToken(token)
	if err != nil {
		return nil, err
	}
	return s.GetUserByID(localUser.ID)
}

func (s *AuthService) validateIdentityToken(token string) (*types.User, error) {
	claims, err := s.identity.Validate(token)
	if err != nil {
		return nil, err
	}

	user, err := s.resolveIdentityUser(claims.Sub, claims.Email)
	if err != nil {
		return nil, err
	}

	if s.orgSvc != nil {
		if err := s.orgSvc.SyncIdentityMemberships(user.ID, token); err != nil {
			return nil, fmt.Errorf("provision org membership: %w", err)
		}
	}

	return user, nil
}

func (s *AuthService) resolveIdentityUser(globalID, email string) (*types.User, error) {
	if globalID == "" {
		return nil, fmt.Errorf("missing identity subject")
	}

	var user types.User
	var globalIdentityID sql.NullString
	var createdAt time.Time
	err := s.db.QueryRow(`
		SELECT id, COALESCE(global_identity_id, ''), email, username,
		       COALESCE(presence_status, 'available'), created_at
		FROM users WHERE global_identity_id = ?`, globalID,
	).Scan(&user.ID, &globalIdentityID, &user.Email, &user.Username, &user.PresenceStatus, &createdAt)
	if err == nil {
		user.GlobalIdentityID = globalIdentityID.String
		user.CreatedAt = createdAt
		return &user, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	if email != "" {
		err = s.db.QueryRow(`
			SELECT id, COALESCE(global_identity_id, ''), email, username,
			       COALESCE(presence_status, 'available'), created_at
			FROM users WHERE email = ?`, email,
		).Scan(&user.ID, &globalIdentityID, &user.Email, &user.Username, &user.PresenceStatus, &createdAt)
		if err == nil {
			if globalIdentityID.Valid && globalIdentityID.String != "" && globalIdentityID.String != globalID {
				return nil, fmt.Errorf("email already linked to another identity")
			}
			_, err = s.db.Exec(
				`UPDATE users SET global_identity_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
				globalID, user.ID,
			)
			if err != nil {
				return nil, err
			}
			user.GlobalIdentityID = globalID
			user.CreatedAt = createdAt
			return &user, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}

	return s.provisionIdentityUser(globalID, email)
}

func (s *AuthService) provisionIdentityUser(globalID, email string) (*types.User, error) {
	if email == "" {
		return nil, fmt.Errorf("identity user missing email")
	}

	userID := ids.NewUUID()
	username, err := s.uniqueUsernameFromEmail(email)
	if err != nil {
		return nil, err
	}

	_, err = s.db.Exec(`
		INSERT INTO users (id, email, username, password_hash, display_name, global_identity_id, presence_status)
		VALUES (?, ?, ?, ?, ?, ?, 'available')`,
		userID, email, username, identityPasswordSentinel, username, globalID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to provision identity user: %w", err)
	}

	return &types.User{
		ID:               userID,
		GlobalIdentityID: globalID,
		Email:            email,
		Username:         username,
		DisplayName:      username,
		PresenceStatus:   "available",
		CreatedAt:        time.Now(),
	}, nil
}

func (s *AuthService) uniqueUsernameFromEmail(email string) (string, error) {
	base := strings.Split(email, "@")[0]
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, base)
	if base == "" {
		base = "user"
	}
	if len(base) > 24 {
		base = base[:24]
	}

	candidate := base
	for i := 0; i < 20; i++ {
		var count int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, candidate).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		suffix := ids.NewUUID()[:8]
		candidate = fmt.Sprintf("%s_%s", base, suffix)
	}
	return "", fmt.Errorf("could not generate unique username")
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
	accessToken, err := s.local.GenerateAccessToken(&types.User{ID: userID, Email: email, Username: username})
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := s.local.GenerateRefreshToken()
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
		ID:             userID,
		Email:          email,
		Username:       username,
		PresenceStatus: "available",
		CreatedAt:      time.Now(),
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

	if passwordHash == identityPasswordSentinel {
		return nil, fmt.Errorf("use identity login for this account")
	}

	// Verify password
	if !verifyPassword(password, passwordHash) {
		return nil, fmt.Errorf("invalid credentials")
	}

	// Create tokens
	user := &types.User{ID: userID, Email: email, Username: username}
	accessToken, err := s.local.GenerateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, err := s.local.GenerateRefreshToken()
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
	var email, username, presenceStatus string
	var globalIdentityID sql.NullString
	var createdAt time.Time
	err := s.db.QueryRow(
		`SELECT id, COALESCE(global_identity_id, ''), email, username,
		        COALESCE(presence_status, 'available'), created_at
		 FROM users WHERE id = ?`, id,
	).Scan(&id, &globalIdentityID, &email, &username, &presenceStatus, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	if presenceStatus == "" {
		presenceStatus = "available"
	}

	return &types.User{
		ID:               id,
		GlobalIdentityID: globalIdentityID.String,
		Email:            email,
		Username:         username,
		PresenceStatus:   presenceStatus,
		CreatedAt:        createdAt,
	}, nil
}

// UpdatePresenceStatus updates a user's presence status
func (s *AuthService) UpdatePresenceStatus(userID, status string) (*types.User, error) {
	switch status {
	case "available", "offline", "dnd":
	default:
		return nil, fmt.Errorf("invalid presence status")
	}

	_, err := s.db.Exec(
		"UPDATE users SET presence_status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update presence: %w", err)
	}

	return s.GetUserByID(userID)
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

func generateUniqueID() (string, error) {
	return ids.NewUUID(), nil
}
