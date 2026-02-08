package config

import (
	"os"
)

type Config struct {
	Port string

	LogLevel string
	Env      string

	DatabaseURL string
	RedisURL    string
}

func LoadConfig() (*Config, error) {
	return &Config{
		Port:        GetEnv("PORT", "8081"),
		DatabaseURL: GetEnv("DATABASE_URL", "postgres://echostream:password@localhost:5432/echostream?sslmode=disable"),
		RedisURL:    GetEnv("REDIS_URL", "redis://localhost:6379"),
		Env:         GetEnv("ENV", "development"),
		LogLevel:    GetEnv("LOG_LEVEL", "info"),
	}, nil
}

func GetEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
