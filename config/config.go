package config

import (
	"os"
)

type Config struct {
	Port      string
	DBPath    string
	JWTSecret string
	ServerURL string

	IdentityURL    string
	IdentityIssuer string
	IdentityJWKS   string
	IdentityAPIKey string
	AuthMode       string // local, identity, dual
	SetupToken     string
}

const (
	DefaultPort      = "8090"
	DefaultDBPath    = "./data/cocina.db"
	DefaultJWTSecret = "cocina-mvp-secret-key-change-in-production"
	DefaultServerURL = "http://localhost:8090/api/v1"
	DefaultAuthMode  = "dual"
)

func Load() *Config {
	port := getEnv("COCINA_PORT", DefaultPort)
	serverURL := getEnv("COCINA_SERVER_URL", "")
	if serverURL == "" {
		serverURL = "http://localhost:" + port + "/api/v1"
	}

	identityURL := getEnv("COCINA_IDENTITY_URL", "")
	identityJWKS := getEnv("COCINA_IDENTITY_JWKS_URL", "")
	if identityJWKS == "" && identityURL != "" {
		identityJWKS = trimRightSlash(identityURL) + "/.well-known/jwks.json"
	}

	authMode := getEnv("COCINA_AUTH_MODE", DefaultAuthMode)
	if identityURL == "" && authMode != "local" {
		authMode = "local"
	}

	return &Config{
		Port:           port,
		DBPath:         getEnv("COCINA_DB_PATH", DefaultDBPath),
		JWTSecret:      getEnv("COCINA_JWT_SECRET", DefaultJWTSecret),
		ServerURL:      serverURL,
		IdentityURL:    identityURL,
		IdentityIssuer: getEnv("COCINA_IDENTITY_ISSUER", identityURL),
		IdentityJWKS:   identityJWKS,
		IdentityAPIKey: getEnv("COCINA_IDENTITY_API_KEY", ""),
		AuthMode:       authMode,
		SetupToken:     getEnv("COCINA_SETUP_TOKEN", ""),
	}
}

func (c *Config) IdentityEnabled() bool {
	return c.IdentityURL != "" && c.AuthMode != "local"
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
