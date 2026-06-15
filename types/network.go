package types

// Network exposure modes for self-hosted installs.
const (
	ExposureLocal  = "local"
	ExposurePublic = "public"
	ExposureTunnel = "tunnel"
)

// NetworkSettings describes how this org server is reachable from the internet.
type NetworkSettings struct {
	ExposureMode      string      `json:"exposure_mode"`
	PublicBaseURL     string      `json:"public_base_url"`
	PublicWSURL       string      `json:"public_ws_url"`
	IdentityPublicURL string      `json:"identity_public_url"`
	ServerAPIURL      string      `json:"server_api_url"`
	SetupComplete     bool        `json:"setup_complete"`
	LastSyncAt        string      `json:"last_sync_at,omitempty"`
	LastHealthOK      bool        `json:"last_health_ok"`
	IdentitySyncOK    bool        `json:"identity_sync_ok,omitempty"`
	IdentitySyncError string      `json:"identity_sync_error,omitempty"`
	ClientHints       ClientHints `json:"client_hints"`
	Guide             NetworkGuide `json:"guide,omitempty"`
}

type ClientHints struct {
	ViteDiscoveryURL string `json:"vite_discovery_url"`
	ViteAPIURL       string `json:"vite_api_url"`
	ViteWSURL        string `json:"vite_ws_url"`
}

type NetworkGuide struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps"`
}

type NetworkTestResult struct {
	ServerReachable   bool   `json:"server_reachable"`
	ServerError       string `json:"server_error,omitempty"`
	IdentityReachable bool   `json:"identity_reachable"`
	IdentityError     string `json:"identity_error,omitempty"`
}

type SaveNetworkInput struct {
	ExposureMode      string `json:"exposure_mode"`
	PublicBaseURL     string `json:"public_base_url"`
	IdentityPublicURL string `json:"identity_public_url"`
	SyncIdentity      bool   `json:"sync_identity"`
}
