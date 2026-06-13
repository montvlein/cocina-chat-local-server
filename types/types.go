package types

import "time"

// User represents a registered user in the system
type User struct {
	ID               string    `json:"id"`
	GlobalIdentityID string    `json:"global_identity_id,omitempty"`
	Email            string    `json:"email"`
	Username         string    `json:"username"`
	Password         string    `json:"-"` // Never expose password in JSON
	DisplayName      string    `json:"display_name,omitempty"`
	PresenceStatus   string    `json:"presence_status"`
	CreatedAt        time.Time `json:"created_at"`
}

// Message represents a chat message between users
type Message struct {
	ID          string    `json:"id"`
	SenderID    string    `json:"sender_id"`
	SenderName  string    `json:"sender_name,omitempty"`
	ReceiverID  string    `json:"receiver_id,omitempty"`
	ChannelID   string    `json:"channel_id,omitempty"`
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"` // "text", "rich"
	IsRead      bool      `json:"is_read"`
	CreatedAt   time.Time `json:"created_at"`
}

// AuthResponse is returned after successful authentication
type AuthResponse struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// WSMessage is sent over WebSocket connections
type WSMessage struct {
	Type      string                 `json:"type"` // "message", "typing", "presence"
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}
