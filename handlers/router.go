package handlers

import (
	"net/http"
	"strings"
)

// DispatchAPI routes nested /api/v1/ paths.
func (h *APIHandler) DispatchAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch {
	case path == "orgs" || path == "orgs/":
		h.ListOrgs(w, r)
		return

	case len(parts) >= 3 && parts[0] == "orgs" && parts[2] == "workspaces":
		h.ListWorkspaces(w, r, parts[1])
		return

	case len(parts) >= 3 && parts[0] == "workspaces" && parts[2] == "channels":
		h.ListWorkspaceChannels(w, r, parts[1])
		return

	case len(parts) >= 3 && parts[0] == "workspaces" && parts[2] == "dms":
		h.CreateOrGetDM(w, r, parts[1])
		return

	case len(parts) >= 3 && parts[0] == "channels" && parts[2] == "messages":
		h.ChannelMessages(w, r, parts[1])
		return
	}

	h.writeAPIError(w, r, "NOT_FOUND", "Endpoint not found", http.StatusNotFound)
}
