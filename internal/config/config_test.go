package config

import (
	"strings"
	"testing"
)

func TestProductionConfiguration(t *testing.T) {
	good := map[string]string{
		"ENV": "production", "PORT": "8081", "ADMIN_PORT": "", "JWT_SECRET": strings.Repeat("ab", 24),
		"DATABASE_URL": "postgres://db/chat", "REDIS_URL": "redis://cache",
		"FRONTEND_BASE_URL": "https://chat.example.org", "CORS_ALLOWED_ORIGINS": "https://chat.example.org",
	}
	for k, v := range good {
		t.Setenv(k, v)
	}
	if _, err := LoadConfig(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ key, value string }{
		{"ENV", "prodution"}, {"JWT_SECRET", ""}, {"JWT_SECRET", composeSecret}, {"JWT_SECRET", "short"},
		{"DATABASE_URL", ""}, {"REDIS_URL", ""}, {"CORS_ALLOWED_ORIGINS", ","}, {"CORS_ALLOWED_ORIGINS", "*"},
		{"CORS_ALLOWED_ORIGINS", "https://chat.example.org/path"}, {"FRONTEND_BASE_URL", "http://chat.example.org"},
		{"PORT", "65536"}, {"ADMIN_PORT", "8081"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := LoadConfig(); err == nil {
				t.Error("configuration accepted")
			}
		})
	}
}
