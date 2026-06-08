package config

import (
	"os"
)

type Config struct {
	Port      string
	DBPath    string
	JWTSecret string
	ServerURL string
}

const (
	DefaultPort      = "8090"
	DefaultDBPath    = "./data/cocina.db"
	DefaultJWTSecret = "cocina-mvp-secret-key-change-in-production"
	DefaultServerURL = "http://localhost:8090/api/v1"
)

func Load() *Config {
	port := getEnv("COCINA_PORT", DefaultPort)
	serverURL := getEnv("COCINA_SERVER_URL", "")
	if serverURL == "" {
		serverURL = "http://localhost:" + port + "/api/v1"
	}
	return &Config{
		Port:      port,
		DBPath:    getEnv("COCINA_DB_PATH", DefaultDBPath),
		JWTSecret: getEnv("COCINA_JWT_SECRET", DefaultJWTSecret),
		ServerURL: serverURL,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
