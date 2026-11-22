package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const devSecret = "dev-secret-do-not-use-in-prod"

type Config struct {
	Port, AdminPort, LogLevel, Env   string
	DatabaseURL, RedisURL, JWTSecret string
	TrustedProxies                   []string
	CORSAllowedOrigins               []string
	FrontendBaseURL                  string
}

func LoadConfig() (*Config, error) {
	cfg := &Config{
		Env: GetEnv("ENV", "development"), Port: GetEnv("PORT", "8081"),
		AdminPort: os.Getenv("ADMIN_PORT"), LogLevel: GetEnv("LOG_LEVEL", "info"),
		DatabaseURL:        GetEnv("DATABASE_URL", "postgres://echostream:echostream123@localhost:5432/echostream?sslmode=disable"),
		RedisURL:           GetEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:          GetEnv("JWT_SECRET", devSecret),
		FrontendBaseURL:    strings.TrimRight(GetEnv("FRONTEND_BASE_URL", "http://localhost:5173"), "/"),
		TrustedProxies:     splitAndTrim(os.Getenv("TRUSTED_PROXY_CIDRS")),
		CORSAllowedOrigins: splitAndTrim(GetEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:3000")),
	}
	switch cfg.Env {
	case "development", "test":
	case "production":
		for _, name := range []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "FRONTEND_BASE_URL", "CORS_ALLOWED_ORIGINS"} {
			if strings.TrimSpace(os.Getenv(name)) == "" {
				return nil, fmt.Errorf("%s is required in production", name)
			}
		}
		if len(strings.TrimSpace(cfg.JWTSecret)) < 32 || strings.TrimSpace(cfg.JWTSecret) == devSecret {
			return nil, fmt.Errorf("JWT_SECRET must contain at least 32 bytes in production")
		}
		if len(cfg.CORSAllowedOrigins) == 0 {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS must contain at least one HTTPS origin")
		}
	default:
		return nil, fmt.Errorf("ENV must be development, test, or production")
	}
	for _, origin := range append(append([]string(nil), cfg.CORSAllowedOrigins...), cfg.FrontendBaseURL) {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (cfg.Env == "production" || u.Scheme != "http")) {
			return nil, fmt.Errorf("configure explicit HTTP origins, using HTTPS in production")
		}
	}
	for _, port := range []string{cfg.Port, cfg.AdminPort} {
		if port == "" {
			continue
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid listener port")
		}
	}
	if cfg.Port == cfg.AdminPort {
		return nil, fmt.Errorf("PORT and ADMIN_PORT must differ")
	}
	return cfg, nil
}

func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func splitAndTrim(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
