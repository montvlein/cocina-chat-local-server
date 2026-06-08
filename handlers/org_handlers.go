package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/cocina/server-mvp/types"
)

// ListOrgs returns organizations for the authenticated user.
func (h *APIHandler) ListOrgs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized)
		return
	}

	orgs, err := h.orgSvc.ListOrgsForUser(userID)
	if err != nil {
		log.Printf("List orgs error: %v", err)
		h.writeAPIError(w, r, "INTERNAL_ERROR", "Failed to list organizations", http.StatusInternalServerError)
		return
	}
	if orgs == nil {
		orgs = []types.OrgMembership{}
	}
	h.writeData(w, http.StatusOK, orgs)
}

// ListWorkspaces returns workspaces for an organization.
func (h *APIHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request, orgID string) {
	if r.Method != http.MethodGet {
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized)
		return
	}

	workspaces, err := h.orgSvc.ListWorkspaces(userID, orgID)
	if err != nil {
		if err.Error() == "not an organization member" {
			h.writeAPIError(w, r, "NOT_FOUND", "Organization not found", http.StatusNotFound)
			return
		}
		log.Printf("List workspaces error: %v", err)
		h.writeAPIError(w, r, "INTERNAL_ERROR", "Failed to list workspaces", http.StatusInternalServerError)
		return
	}
	if workspaces == nil {
		workspaces = []types.Workspace{}
	}
	h.writeData(w, http.StatusOK, workspaces)
}

// ListWorkspaceChannels returns channels in a workspace.
func (h *APIHandler) ListWorkspaceChannels(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if r.Method != http.MethodGet {
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized)
		return
	}

	channels, err := h.orgSvc.ListChannels(userID, workspaceID)
	if err != nil {
		if err.Error() == "not a workspace member" {
			h.writeAPIError(w, r, "NOT_FOUND", "Workspace not found", http.StatusNotFound)
			return
		}
		log.Printf("List channels error: %v", err)
		h.writeAPIError(w, r, "INTERNAL_ERROR", "Failed to list channels", http.StatusInternalServerError)
		return
	}
	if channels == nil {
		channels = []types.Channel{}
	}
	h.writeData(w, http.StatusOK, channels)
}

// CreateOrGetDM creates or returns a DM channel in a workspace.
func (h *APIHandler) CreateOrGetDM(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if r.Method != http.MethodPost {
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.UserIDs) == 0 {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "user_ids is required", http.StatusBadRequest)
		return
	}

	channel, err := h.orgSvc.GetOrCreateDM(userID, workspaceID, req.UserIDs)
	if err != nil {
		log.Printf("Create DM error: %v", err)
		h.writeAPIError(w, r, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(types.APIEnvelope{Data: channel})
}

// ChannelMessages handles GET/POST for /channels/:id/messages
func (h *APIHandler) ChannelMessages(w http.ResponseWriter, r *http.Request, channelID string) {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Unauthorized", http.StatusUnauthorized)
		return
	}

	canAccess, err := h.orgSvc.UserCanAccessChannel(userID, channelID)
	if err != nil || !canAccess {
		h.writeAPIError(w, r, "NOT_FOUND", "Channel not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getChannelMessages(w, r, channelID)
	case http.MethodPost:
		h.postChannelMessage(w, r, userID, channelID)
	default:
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) getChannelMessages(w http.ResponseWriter, r *http.Request, channelID string) {
	limit := 50
	beforeID := r.URL.Query().Get("before")
	if beforeID == "" {
		beforeID = r.URL.Query().Get("cursor")
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if limit > 100 {
		limit = 100
	}

	fetchLimit := limit + 1
	messages, err := h.msgSvc.GetChannelMessages(channelID, fetchLimit, beforeID)
	if err != nil {
		log.Printf("Get channel messages error: %v", err)
		h.writeAPIError(w, r, "INTERNAL_ERROR", "Failed to get messages", http.StatusInternalServerError)
		return
	}

	hasMore := len(messages) > limit
	nextCursor := ""
	if hasMore {
		messages = messages[:limit]
	}
	if len(messages) > 0 {
		nextCursor = messages[0].ID
	}
	reverseMessages(messages)

	h.writePaginated(w, messages, types.APIMeta{
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func (h *APIHandler) postChannelMessage(w http.ResponseWriter, r *http.Request, userID, channelID string) {
	req, err := decodeSendMessageRequest(r)
	if err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Content == "" {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Content is required", http.StatusBadRequest)
		return
	}
	if req.ContentType == "" {
		req.ContentType = "text"
	}

	receiverID := req.ReceiverID
	ch, _ := h.orgSvc.GetChannelByID(channelID, userID)
	if ch != nil && ch.Type == types.ChannelTypeDM && ch.ParticipantID != "" {
		receiverID = ch.ParticipantID
	}

	msg, err := h.msgSvc.SendMessage(userID, receiverID, channelID, req.Content, req.ContentType)
	if err != nil {
		log.Printf("Send channel message error: %v", err)
		h.writeAPIError(w, r, "INTERNAL_ERROR", "Failed to send message", http.StatusInternalServerError)
		return
	}

	senderName := ""
	if sender, err := h.auth.GetUserByID(userID); err == nil {
		senderName = sender.Username
		msg.SenderName = senderName
	}

	h.broadcastMessage(userID, msg, senderName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(types.APIEnvelope{Data: msg})
}
