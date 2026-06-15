package handlers

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/cocina/server-mvp/network"
	"github.com/cocina/server-mvp/types"
)

func (h *APIHandler) DispatchAdmin(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/")
	path = strings.Trim(path, "/")
	switch path {
	case "network", "":
		switch r.Method {
		case http.MethodGet:
			h.GetNetworkSettings(w, r)
		case http.MethodPost, http.MethodPut:
			h.SaveNetworkSettings(w, r)
		default:
			h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
		}
	case "network/test":
		if r.Method != http.MethodPost {
			h.writeAPIError(w, r, "METHOD_NOT_ALLOWED", "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.TestNetworkSettings(w, r)
	default:
		h.writeAPIError(w, r, "NOT_FOUND", "Endpoint not found", http.StatusNotFound)
	}
}

func (h *APIHandler) GetNetworkSettings(w http.ResponseWriter, r *http.Request) {
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
	if !h.canManageNetwork(r) {
		h.writeAPIError(w, r, "FORBIDDEN", "Admin access required (Bearer owner/admin or X-Setup-Token)", http.StatusForbidden)
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

func (h *APIHandler) canManageNetwork(r *http.Request) bool {
	if isLocalAdminHost(r) {
		return true
	}

	if h.cfg.SetupToken != "" && r.Header.Get("X-Setup-Token") == h.cfg.SetupToken {
		return true
	}

	store := network.NewStore(h.db)
	setupComplete, _ := store.GetSetting("network_setup_complete")
	if setupComplete != "true" {
		return true
	}

	userID := h.extractUserID(r)
	if userID == "" {
		return false
	}
	return h.orgSvc.UserIsOrgAdmin(userID)
}

func isLocalAdminHost(r *http.Request) bool {
	host := r.Host
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	host = strings.Trim(strings.ToLower(host), "[]")

	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
