package observ

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger creates a new zap.Logger instance based on the provided environment and log level.
func NewLogger(env, level string) (*zap.Logger, error) {
	var config zap.Config

	if env == "production" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
	}

	zapLevel, err := zapcore.ParseLevel(level)
	if err != nil {
		zapLevel = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	return config.Build()
}
