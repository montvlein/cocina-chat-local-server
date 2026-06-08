package types

import "time"

const (
	ChannelTypePublic  = "public"
	ChannelTypePrivate = "private"
	ChannelTypeDM      = "dm"
	ChannelTypeGroup   = "group"

	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"

	DeploymentSaaS   = "saas"
	DeploymentLocal  = "local"
	DeploymentHybrid = "hybrid"
)

// Organization represents a tenant.
type Organization struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	ServerURL      string    `json:"server_url"`
	DeploymentMode string    `json:"deployment_mode"`
	CreatedAt      time.Time `json:"created_at"`
}

// OrgMembership wraps an org with the user's role.
type OrgMembership struct {
	Org      Organization `json:"org"`
	Role     string       `json:"role"`
	JoinedAt time.Time    `json:"joined_at"`
}

// Workspace belongs to an organization.
type Workspace struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
}

// Channel belongs to a workspace.
type Channel struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	// Populated for DM channels in list responses
	ParticipantID   string `json:"participant_id,omitempty"`
	ParticipantName string `json:"participant_name,omitempty"`
}

// APIEnvelope is the standard success response wrapper.
type APIEnvelope struct {
	Data interface{} `json:"data"`
	Meta *APIMeta    `json:"meta,omitempty"`
}

// APIMeta holds pagination metadata.
type APIMeta struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more,omitempty"`
}

// APIErrorBody is the structured error format.
type APIErrorBody struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	Details   map[string]interface{} `json:"details,omitempty"`
	RequestID string                 `json:"request_id,omitempty"`
}

// APIErrorResponse wraps API errors.
type APIErrorResponse struct {
	Error APIErrorBody `json:"error"`
}
