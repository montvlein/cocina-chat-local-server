package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/cocina/server-mvp/network"
	"github.com/cocina/server-mvp/org"
	"github.com/cocina/server-mvp/types"
	"github.com/cocina/server-mvp/version"
)

func (h *APIHandler) DispatchAdmin(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	path = strings.Trim(path, "/")

	switch {
	case path == "session" || path == "":
		if r.Method == http.MethodGet {
			h.GetAdminSession(w, r)
			return
		}
	case path == "bootstrap":
		if r.Method == http.MethodPost {
			h.BootstrapAdmin(w, r)
			return
		}
	case path == "status":
		if r.Method == http.MethodGet {
			h.GetAdminStatus(w, r)
			return
		}
	case path == "users":
		switch r.Method {
		case http.MethodGet:
			h.ListAdminUsers(w, r)
		default:
			h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	case strings.HasPrefix(path, "users/") && strings.HasSuffix(path, "/role"):
		if r.Method == http.MethodPatch || r.Method == http.MethodPut {
			userID := strings.TrimSuffix(strings.TrimPrefix(path, "users/"), "/role")
			h.UpdateUserRole(w, r, userID)
			return
		}
	case path == "invitations":
		switch r.Method {
		case http.MethodGet:
			h.ListAdminInvitations(w, r)
		case http.MethodPost:
			h.CreateAdminInvitation(w, r)
		default:
			h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	case strings.HasPrefix(path, "invitations/"):
		invID := strings.TrimPrefix(path, "invitations/")
		if r.Method == http.MethodDelete {
			h.RevokeAdminInvitation(w, r, invID)
			return
		}
	case path == "network":
		switch r.Method {
		case http.MethodGet:
			h.GetNetworkSettings(w, r)
		case http.MethodPost, http.MethodPut:
			h.SaveNetworkSettings(w, r)
		default:
			h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	case path == "network/test":
		if r.Method == http.MethodPost {
			h.TestNetworkSettings(w, r)
			return
		}
	}

	h.writeAPIError(w, r, "NOT_FOUND", "Endpoint not found", http.StatusNotFound)
}

func (h *APIHandler) GetAdminSession(w http.ResponseWriter, r *http.Request) {
	info := types.AdminSessionInfo{
		NeedsSetup: !h.orgSvc.HasAnyOrgAdmin(),
	}

	userID := h.extractUserID(r)
	if userID != "" {
		user, err := h.auth.GetUserByID(userID)
		if err == nil {
			info.User = user
			info.IsAdmin = h.orgSvc.UserIsOrgAdmin(userID)
		}
	}

	h.writeData(w, http.StatusOK, info)
}

func (h *APIHandler) BootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	if h.orgSvc.HasAnyOrgAdmin() {
		h.writeAPIError(w, r, "FORBIDDEN", "Administrator already exists", http.StatusForbidden)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Username == "" || req.Password == "" {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Email, username and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	resp, err := h.auth.Register(req.Email, req.Username, req.Password)
	if err != nil {
		h.writeAPIError(w, r, "CONFLICT", err.Error(), http.StatusConflict)
		return
	}

	if err := h.orgSvc.BootstrapFirstAdmin(resp.User.ID); err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *APIHandler) GetAdminStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	orgID, _ := h.orgSvc.AdminOrgID()
	org, _ := h.orgSvc.GetOrgByID(orgID)
	orgName := "Cocina"
	if org != nil {
		orgName = org.Name
	}

	userCount, _ := h.orgSvc.CountUsers()
	store := network.NewStore(h.db)
	netSettings, _ := store.Load(h.cfg.ServerURL, h.cfg.IdentityURL)
	setupComplete, _ := store.GetSetting("network_setup_complete")
	linkedOrgID, _ := store.GetSetting("identity_org_id")

	status := types.AdminStatus{
		Version:        version.Version,
		AuthMode:       h.cfg.AuthMode,
		IdentityLinked: linkedOrgID != "",
		IdentityURL:    h.cfg.IdentityURL,
		ServerURL:      h.cfg.ServerURL,
		UserCount:      userCount,
		OrgName:        orgName,
		NetworkSetup:   setupComplete == "true",
	}
	if netSettings != nil {
		status.PublicBaseURL = netSettings.PublicBaseURL
	}

	h.writeData(w, http.StatusOK, status)
}

func (h *APIHandler) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	orgID, err := h.orgSvc.AdminOrgID()
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	users, err := h.orgSvc.ListOrgUsers(orgID)
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeData(w, http.StatusOK, users)
}

func (h *APIHandler) UpdateUserRole(w http.ResponseWriter, r *http.Request, targetUserID string) {
	actorID := h.extractUserID(r)
	if actorID == "" || !h.orgSvc.UserIsOrgAdmin(actorID) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required", http.StatusForbidden)
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	orgID, err := h.orgSvc.AdminOrgID()
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.orgSvc.SetOrgMemberRole(actorID, targetUserID, orgID, req.Role); err != nil {
		code := "INTERNAL_ERROR"
		status := http.StatusInternalServerError
		if err.Error() == "forbidden" || err.Error() == "cannot demote yourself" {
			code = "FORBIDDEN"
			status = http.StatusForbidden
		} else if err.Error() == "invalid role" {
			code = "VALIDATION_ERROR"
			status = http.StatusBadRequest
		}
		h.writeAPIError(w, r, code, err.Error(), status)
		return
	}

	h.writeData(w, http.StatusOK, map[string]string{"user_id": targetUserID, "role": req.Role})
}

func (h *APIHandler) ListAdminInvitations(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUserID(r)
	if userID == "" || !h.orgSvc.UserIsOrgAdmin(userID) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required", http.StatusForbidden)
		return
	}

	orgID, err := h.orgSvc.AdminOrgID()
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	list, err := h.orgSvc.ListInvitations(orgID, userID)
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeData(w, http.StatusOK, list)
}

func (h *APIHandler) CreateAdminInvitation(w http.ResponseWriter, r *http.Request) {
	userID := h.extractUserID(r)
	if userID == "" || !h.orgSvc.UserIsOrgAdmin(userID) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required", http.StatusForbidden)
		return
	}

	var req struct {
		Role          string `json:"role"`
		ExpiresInDays int    `json:"expires_in_days"`
		MaxUses       int    `json:"max_uses"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	orgID, err := h.orgSvc.AdminOrgID()
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	store := network.NewStore(h.db)
	settings, _ := store.Load(h.cfg.ServerURL, h.cfg.IdentityURL)
	publicBase := ""
	if settings != nil {
		publicBase = settings.PublicBaseURL
	}

	resp, err := h.orgSvc.CreateInvitation(orgID, userID, publicBase, org.CreateInvitationInput{
		Role:          req.Role,
		ExpiresInDays: req.ExpiresInDays,
		MaxUses:       req.MaxUses,
	})
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	h.writeData(w, http.StatusCreated, resp)
}

func (h *APIHandler) RevokeAdminInvitation(w http.ResponseWriter, r *http.Request, invID string) {
	userID := h.extractUserID(r)
	if userID == "" || !h.orgSvc.UserIsOrgAdmin(userID) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required", http.StatusForbidden)
		return
	}

	orgID, err := h.orgSvc.AdminOrgID()
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.orgSvc.RevokeInvitation(orgID, invID, userID); err != nil {
		h.writeAPIError(w, r, "NOT_FOUND", err.Error(), http.StatusNotFound)
		return
	}

	h.writeData(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (h *APIHandler) GetNetworkSettings(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	store := network.NewStore(h.db)
	settings, err := store.Load(h.cfg.ServerURL, h.cfg.IdentityURL)
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}
	h.writeData(w, http.StatusOK, stripAdminExtras(settings))
}

func stripAdminExtras(s *types.NetworkSettings) *types.NetworkSettings {
	if s == nil {
		return s
	}
	s.Guide = types.NetworkGuide{}
	s.ClientHints = types.ClientHints{}
	return s
}

func (h *APIHandler) SaveNetworkSettings(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	var input types.SaveNetworkInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", "Invalid request body", http.StatusBadRequest)
		return
	}

	store := network.NewStore(h.db)
	settings, err := store.Save(input, h.cfg.ServerURL, h.cfg.IdentityURL)
	if err != nil {
		h.writeAPIError(w, r, "VALIDATION_ERROR", err.Error(), http.StatusBadRequest)
		return
	}

	deploymentMode := network.DeploymentModeForExposure(settings.ExposureMode)
	if err := h.orgSvc.UpdateNetworkURLs(settings.ServerAPIURL, settings.IdentityPublicURL, deploymentMode); err != nil {
		log.Printf("network: update org urls: %v", err)
	}

	if input.SyncIdentity {
		org, err := h.orgSvc.RegisterServerWithIdentity(settings.ServerAPIURL)
		if err != nil {
			settings.IdentitySyncOK = false
			settings.IdentitySyncError = err.Error()
			log.Printf("network: identity sync failed: %v", err)
		} else {
			settings.IdentitySyncOK = true
			settings.ServerAPIURL = org.ServerURL
			if org.ServerURL != "" {
				settings.PublicBaseURL = strings.TrimSuffix(org.ServerURL, "/api/v1")
				settings.PublicWSURL = network.WebSocketURL(settings.PublicBaseURL)
			}
		}
	}

	test := network.ProbeSettings(settings)
	settings.LastHealthOK = test.ServerReachable
	store.SetLastHealthOK(test.ServerReachable)

	h.writeData(w, http.StatusOK, stripAdminExtras(settings))
}

func (h *APIHandler) TestNetworkSettings(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	store := network.NewStore(h.db)
	settings, err := store.Load(h.cfg.ServerURL, h.cfg.IdentityURL)
	if err != nil {
		h.writeAPIError(w, r, "INTERNAL_ERROR", err.Error(), http.StatusInternalServerError)
		return
	}

	var override types.SaveNetworkInput
	_ = json.NewDecoder(r.Body).Decode(&override)
	if override.ExposureMode != "" {
		settings.ExposureMode = override.ExposureMode
	}
	if override.PublicBaseURL != "" {
		settings.PublicBaseURL = network.NormalizeBaseURL(override.PublicBaseURL)
		settings.ServerAPIURL = network.ServerAPIURL(settings.PublicBaseURL)
		settings.PublicWSURL = network.WebSocketURL(settings.PublicBaseURL)
	}
	if override.IdentityPublicURL != "" {
		settings.IdentityPublicURL = network.NormalizeBaseURL(override.IdentityPublicURL)
	}

	result := network.ProbeSettings(settings)
	h.writeData(w, http.StatusOK, result)
}

func (h *APIHandler) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Login required", http.StatusUnauthorized)
		return false
	}
	if !h.orgSvc.UserIsOrgAdmin(userID) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required", http.StatusForbidden)
		return false
	}
	return true
}

func (h *APIHandler) DispatchInvitation(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/invitations/")
	path = strings.Trim(path, "/")
	if path == "" {
		h.writeAPIError(w, r, "NOT_FOUND", "Endpoint not found", http.StatusNotFound)
		return
	}

	if strings.HasSuffix(path, "/accept") {
		token := strings.TrimSuffix(path, "/accept")
		if r.Method == http.MethodPost {
			h.AcceptInvitation(w, r, token)
			return
		}
		h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == http.MethodGet {
		h.PreviewInvitation(w, r, path)
		return
	}

	h.writeAPIError(w, r, "NOT_FOUND", "Endpoint not found", http.StatusNotFound)
}

func (h *APIHandler) PreviewInvitation(w http.ResponseWriter, r *http.Request, token string) {
	preview, err := h.orgSvc.PreviewInvitation(token)
	if err != nil {
		h.writeAPIError(w, r, "NOT_FOUND", err.Error(), http.StatusNotFound)
		return
	}
	h.writeData(w, http.StatusOK, preview)
}

func (h *APIHandler) AcceptInvitation(w http.ResponseWriter, r *http.Request, token string) {
	userID := h.extractUserID(r)
	if userID == "" {
		h.writeAPIError(w, r, "UNAUTHORIZED", "Login required", http.StatusUnauthorized)
		return
	}

	if err := h.orgSvc.AcceptInvitation(token, userID); err != nil {
		h.writeAPIError(w, r, "BAD_REQUEST", err.Error(), http.StatusBadRequest)
		return
	}

	h.writeData(w, http.StatusOK, map[string]bool{"accepted": true})
}
