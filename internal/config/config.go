package config

import (
	"os"
	"strings"
)

type Config struct {
	Port string

	LogLevel string
	Env      string

	DatabaseURL string
	RedisURL    string

	JWTSecret string

	// CORSAllowedOrigins is the list of browser origins permitted to call the API.
	CORSAllowedOrigins []string

	// FrontendBaseURL is the base URL of the web app; used to build invite links.
	FrontendBaseURL string
}

// LoadConfig reads config from environment variables.
func LoadConfig() (*Config, error) {
	return &Config{
		Port:               GetEnv("PORT", "8081"),
		DatabaseURL:        GetEnv("DATABASE_URL", "postgres://echostream:echostream123@localhost:5432/echostream?sslmode=disable"),
		RedisURL:           GetEnv("REDIS_URL", "redis://localhost:6379"),
		Env:                GetEnv("ENV", "development"),
		LogLevel:           GetEnv("LOG_LEVEL", "info"),
		JWTSecret:          GetEnv("JWT_SECRET", "dev-secret-do-not-use-in-prod"),
		CORSAllowedOrigins: splitAndTrim(GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")),
		FrontendBaseURL:    GetEnv("FRONTEND_BASE_URL", "http://localhost:5173"),
	}, nil
}

// GetEnv returns an env var or a default value.
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// splitAndTrim splits a comma-separated list and drops empty/whitespace entries.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
