package network

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocina/server-mvp/types"
)

const (
	keyExposureMode      = "network_exposure_mode"
	keyPublicBaseURL     = "network_public_base_url"
	keyIdentityPublicURL = "network_identity_public_url"
	keySetupComplete     = "network_setup_complete"
	keyLastSyncAt        = "network_last_sync_at"
	keyLastHealthOK      = "network_last_health_ok"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM server_settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO server_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func DeploymentModeForExposure(mode string) string {
	switch mode {
	case types.ExposurePublic:
		return types.DeploymentPublic
	case types.ExposureTunnel:
		return types.DeploymentTunnel
	default:
		return types.DeploymentLocal
	}
}

func NormalizeBaseURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	return u
}

func ServerAPIURL(base string) string {
	base = NormalizeBaseURL(base)
	if base == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(base), "/api/v1") {
		return base
	}
	return base + "/api/v1"
}

func WebSocketURL(base string) string {
	base = NormalizeBaseURL(base)
	if base == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://")
	default:
		return base
	}
}

func ValidateExposureMode(mode string) error {
	switch mode {
	case types.ExposureLocal, types.ExposurePublic, types.ExposureTunnel:
		return nil
	default:
		return fmt.Errorf("exposure_mode must be local, public or tunnel")
	}
}

func (s *Store) Load(defaultServerURL, defaultIdentityURL string) (*types.NetworkSettings, error) {
	mode, _ := s.GetSetting(keyExposureMode)
	baseURL, _ := s.GetSetting(keyPublicBaseURL)
	identityURL, _ := s.GetSetting(keyIdentityPublicURL)
	setupComplete, _ := s.GetSetting(keySetupComplete)
	lastSync, _ := s.GetSetting(keyLastSyncAt)
	lastHealth, _ := s.GetSetting(keyLastHealthOK)

	if mode == "" {
		mode = types.ExposureLocal
	}
	if baseURL == "" {
		baseURL = strings.TrimSuffix(NormalizeBaseURL(defaultServerURL), "/api/v1")
		if baseURL == "" {
			baseURL = defaultServerURL
		}
	}
	if identityURL == "" {
		identityURL = NormalizeBaseURL(defaultIdentityURL)
	}

	apiURL := ServerAPIURL(baseURL)
	wsURL := WebSocketURL(baseURL)

	return &types.NetworkSettings{
		ExposureMode:      mode,
		PublicBaseURL:     baseURL,
		PublicWSURL:       wsURL,
		IdentityPublicURL: identityURL,
		ServerAPIURL:      apiURL,
		SetupComplete:     setupComplete == "true",
		LastSyncAt:        lastSync,
		LastHealthOK:      lastHealth == "true",
		ClientHints: types.ClientHints{
			ViteDiscoveryURL: identityURL,
			ViteAPIURL:       apiURL,
			ViteWSURL:        wsURL,
		},
		Guide: GuideForMode(mode),
	}, nil
}

func (s *Store) Save(input types.SaveNetworkInput, defaultServerURL, defaultIdentityURL string) (*types.NetworkSettings, error) {
	if err := ValidateExposureMode(input.ExposureMode); err != nil {
		return nil, err
	}

	baseURL := NormalizeBaseURL(input.PublicBaseURL)
	identityURL := NormalizeBaseURL(input.IdentityPublicURL)

	if input.ExposureMode == types.ExposureLocal {
		if baseURL == "" {
			baseURL = strings.TrimSuffix(NormalizeBaseURL(defaultServerURL), "/api/v1")
		}
	} else if baseURL == "" {
		return nil, fmt.Errorf("public_base_url is required for mode %s", input.ExposureMode)
	}

	if identityURL == "" && defaultIdentityURL != "" {
		identityURL = NormalizeBaseURL(defaultIdentityURL)
	}

	if err := s.SetSetting(keyExposureMode, input.ExposureMode); err != nil {
		return nil, err
	}
	if err := s.SetSetting(keyPublicBaseURL, baseURL); err != nil {
		return nil, err
	}
	if err := s.SetSetting(keyIdentityPublicURL, identityURL); err != nil {
		return nil, err
	}
	if err := s.SetSetting(keySetupComplete, "true"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_ = s.SetSetting(keyLastSyncAt, now)

	settings, err := s.Load(defaultServerURL, defaultIdentityURL)
	if err != nil {
		return nil, err
	}
	settings.LastSyncAt = now
	return settings, nil
}

func (s *Store) SetLastHealthOK(ok bool) {
	val := "false"
	if ok {
		val = "true"
	}
	_ = s.SetSetting(keyLastHealthOK, val)
}

func GuideForMode(mode string) types.NetworkGuide {
	switch mode {
	case types.ExposurePublic:
		return types.NetworkGuide{
			Summary: "Servidor público (VM / IP fija). Ideal para AWS, GCP o Azure.",
			Steps: []string{
				"Apuntá un registro DNS A o CNAME a la IP pública de la VM.",
				"Configurá TLS con Caddy, nginx o un balanceador.",
				"Abrí el puerto 443 (y 80 para redirección).",
				"Usá la URL pública en public_base_url y sincronizá con Identity.",
			},
		}
	case types.ExposureTunnel:
		return types.NetworkGuide{
			Summary: "Cloudflare Tunnel. Ideal para correr en tu PC sin abrir puertos.",
			Steps: []string{
				"Creá un túnel en Cloudflare Zero Trust.",
				"Instalá cloudflared en el host o como contenedor Docker.",
				"Mapeá chat.tudominio.com → http://localhost:8090.",
				"Pegá la URL pública aquí y sincronizá con Identity.",
			},
		}
	default:
		return types.NetworkGuide{
			Summary: "Solo red local. Sin exposición a internet.",
			Steps: []string{
				"Los clientes en la misma red usan la IP LAN o localhost.",
				"Passkeys y móviles fuera de la red no funcionarán sin túnel o dominio.",
			},
		}
	}
}

func ProbeHealth(baseURL string, client *http.Client) (bool, string) {
	baseURL = NormalizeBaseURL(baseURL)
	if baseURL == "" {
		return false, "empty url"
	}
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return true, ""
}

func ProbeSettings(settings *types.NetworkSettings) *types.NetworkTestResult {
	client := &http.Client{Timeout: 8 * time.Second}
	result := &types.NetworkTestResult{}

	serverBase := settings.PublicBaseURL
	if settings.ExposureMode == types.ExposureLocal {
		serverBase = strings.TrimSuffix(settings.ServerAPIURL, "/api/v1")
	}

	ok, errMsg := ProbeHealth(serverBase, client)
	result.ServerReachable = ok
	if !ok {
		result.ServerError = errMsg
	}

	if settings.IdentityPublicURL != "" {
		ok, errMsg := ProbeHealth(settings.IdentityPublicURL, client)
		result.IdentityReachable = ok
		if !ok {
			result.IdentityError = errMsg
		}
	}
	return result
}
