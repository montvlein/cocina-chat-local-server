package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/cocina/server-mvp/auth"
	"github.com/cocina/server-mvp/messaging"
	"github.com/cocina/server-mvp/types"
	"github.com/gorilla/websocket"
)

// APIHandler handles HTTP request handlers
type APIHandler struct {
	db     *sql.DB
	auth   *auth.AuthService
	wsHub  *messaging.Hub
	msgSvc *messaging.MessageService
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for MVP
	},
}

// NewAPIHandler creates a new API handler
func NewAPIHandler(db *sql.DB, wsHub *messaging.Hub) *APIHandler {
	tokenSvc := auth.NewTokenService("cocina-mvp-secret-key-change-in-production")
	authSvc := auth.NewAuthService(db, tokenSvc)
	msgSvc := messaging.NewMessageService(db)

	return &APIHandler{
		db:     db,
		auth:   authSvc,
		wsHub:  wsHub,
		msgSvc: msgSvc,
	}
}

// Register handles user registration
func (h *APIHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email       string `json:"email"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Username == "" || req.Password == "" {
		h.writeError(w, "Email, username and password are required", http.StatusBadRequest)
		return
	}

	resp, err := h.auth.Register(req.Email, req.Username, req.Password)
	if err != nil {
		log.Printf("Registration error: %v", err)
		h.writeError(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

// Login handles user login
func (h *APIHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	resp, err := h.auth.Login(req.Email, req.Password)
	if err != nil {
		log.Printf("Login error: %v", err)
		h.writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// RefreshToken handles token refresh (simplified for MVP)
func (h *APIHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// For MVP, we generate a new refresh token
	newRefreshToken, _ := auth.NewTokenService("cocina-mvp-secret-key-change-in-production").GenerateRefreshToken()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"refresh_token": newRefreshToken,
	})
}

// Logout handles user logout
func (h *APIHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.auth.Logout(req.RefreshToken); err != nil {
		log.Printf("Logout error: %v", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetMe returns the current authenticated user's profile
func (h *APIHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := h.auth.GetUserByID(userID)
	if err != nil {
		h.writeError(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// SendMessage handles sending a message via REST API
func (h *APIHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		h.writeError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	req, err := decodeSendMessageRequest(r)
	if err != nil {
		h.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		h.writeError(w, "Content is required", http.StatusBadRequest)
		return
	}

	if req.ReceiverID == "" && req.ChannelID == "" {
		h.writeError(w, "Either receiver_id or channel_id must be provided", http.StatusBadRequest)
		return
	}

	if req.ContentType == "" {
		req.ContentType = "text"
	}

	msg, err := h.msgSvc.SendMessage(userID, req.ReceiverID, req.ChannelID, req.Content, req.ContentType)
	if err != nil {
		log.Printf("Send message error: %v", err)
		h.writeError(w, "Failed to send message", http.StatusInternalServerError)
		return
	}

	senderName := ""
	if sender, err := h.auth.GetUserByID(userID); err == nil {
		senderName = sender.Username
		msg.SenderName = senderName
	}

	// Broadcast via WebSocket if receiver is connected
	wsMsg := messaging.WSOutgoingMessage{
		Type:      "message",
		Timestamp: msg.CreatedAt,
		Payload: map[string]interface{}{
			"sender_id":   msg.SenderID,
			"sender_name": senderName,
			"receiver_id": msg.ReceiverID,
			"channel_id":  msg.ChannelID,
			"content":     msg.Content,
			"id":          msg.ID,
		},
	}

	data, _ := json.Marshal(wsMsg)
	h.wsHub.BroadcastMessage(userID, data)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(msg)
}

// GetMessageHistory handles retrieving message history via REST API
func (h *APIHandler) GetMessageHistory(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	limit := 50
	beforeID := ""
	channelID := r.URL.Query().Get("channel_id")

	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 100 {
		limit = 100
	}

	beforeID = r.URL.Query().Get("before")
	if beforeID == "" {
		beforeID = r.URL.Query().Get("cursor")
	}

	fetchLimit := limit + 1
	var messages []*types.Message
	var err error

	if channelID != "" {
		messages, err = h.msgSvc.GetChannelMessages(channelID, fetchLimit, beforeID)
	} else {
		messages, err = h.msgSvc.GetMessageHistory(userID, fetchLimit, beforeID)
	}
	if err != nil {
		log.Printf("Get messages error: %v", err)
		h.writeError(w, "Failed to get messages", http.StatusInternalServerError)
		return
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}

	reverseMessages(messages)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": messages,
		"has_more": hasMore,
	})
}

func reverseMessages(messages []*types.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

// HandleWebSocket handles WebSocket connections
// NOTE: Authentication happens AFTER the upgrade via the 'identify' message
func (h *APIHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade to WebSocket first - authentication happens after via identify message
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	// Create client and start pumps; registration happens after identify
	client := h.wsHub.NewClient(conn)
	h.wsHub.StartClient(client)

	// Send welcome message - client needs to identify first
	welcomeMsg := messaging.WSOutgoingMessage{
		Type:      "welcome",
		Timestamp: time.Now().UTC(),
		Payload: map[string]interface{}{
			"message": "Please send an 'identify' message with your token",
			"op":      "identify",
		},
	}

	data, _ := json.Marshal(welcomeMsg)
	client.Mu.Lock()
	client.Conn.WriteMessage(websocket.TextMessage, data)
	client.Mu.Unlock()

	// Start reading messages from client (ReadPump is started by StartClient)
}

// Helper methods
func (h *APIHandler) extractUserID(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return ""
	}

	token := parts[1]
	tokenSvc := auth.NewTokenService("cocina-mvp-secret-key-change-in-production")
	user, err := tokenSvc.ValidateAccessToken(token)
	if err != nil {
		return ""
	}

	return user.ID
}

func (h *APIHandler) writeError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(types.ErrorResponse{
		Error:   "error",
		Message: message,
	})
}

type sendMessageRequest struct {
	ReceiverID  string
	ChannelID   string
	Content     string
	ContentType string
}

func decodeSendMessageRequest(r *http.Request) (sendMessageRequest, error) {
	var raw map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return sendMessageRequest{}, err
	}

	return sendMessageRequest{
		ReceiverID:  jsonStringField(raw, "receiver_id", "receiverId"),
		ChannelID:   jsonStringField(raw, "channel_id", "channelId"),
		Content:     jsonStringField(raw, "content"),
		ContentType: jsonStringField(raw, "content_type", "contentType"),
	}, nil
}

func jsonStringField(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value, ok := raw[key]
		if !ok || value == nil {
			continue
		}
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}
