package config

import (
	"os"
)

type Config struct {
	Port     string
	DBPath   string
	JWTSecret string
}

const (
	DefaultPort     = "8090"
	DefaultDBPath   = "./data/cocina.db"
	DefaultJWTSecret = "cocina-mvp-secret-key-change-in-production"
)

func Load() *Config {
	return &Config{
		Port:      getEnv("COCINA_PORT", DefaultPort),
		DBPath:    getEnv("COCINA_DB_PATH", DefaultDBPath),
		JWTSecret: getEnv("COCINA_JWT_SECRET", DefaultJWTSecret),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
