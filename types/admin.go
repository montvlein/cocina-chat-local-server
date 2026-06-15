package types

import "time"

type AdminSessionInfo struct {
	NeedsSetup bool   `json:"needs_setup"`
	IsAdmin    bool   `json:"is_admin"`
	User       *User  `json:"user,omitempty"`
}

type AdminStatus struct {
	Version         string `json:"version"`
	AuthMode        string `json:"auth_mode"`
	IdentityLinked  bool   `json:"identity_linked"`
	IdentityURL     string `json:"identity_url,omitempty"`
	ServerURL       string `json:"server_url"`
	UserCount       int    `json:"user_count"`
	OrgName         string `json:"org_name"`
	NetworkSetup    bool   `json:"network_setup_complete"`
	PublicBaseURL   string `json:"public_base_url,omitempty"`
}

type AdminUser struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"display_name,omitempty"`
	Role           string    `json:"role"`
	PresenceStatus string    `json:"presence_status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Invitation struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"org_id"`
	OrgName     string     `json:"org_name,omitempty"`
	Role        string     `json:"role"`
	TokenPrefix string     `json:"token_prefix"`
	InviteURL   string     `json:"invite_url,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	MaxUses     int        `json:"max_uses"`
	UseCount    int        `json:"use_count"`
	Revoked     bool       `json:"revoked"`
	CreatedAt   time.Time  `json:"created_at"`
}

type CreateInvitationResponse struct {
	Invitation Invitation `json:"invitation"`
	Token      string     `json:"token"`
	InviteURL  string     `json:"invite_url"`
}

type InvitationPreview struct {
	OrgID         string     `json:"org_id"`
	OrgName       string     `json:"org_name"`
	Role          string     `json:"role"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Valid         bool       `json:"valid"`
	InvalidReason string     `json:"invalid_reason,omitempty"`
}
