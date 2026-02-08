package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/config"
	"github.com/lalith-99/echostream/internal/observ"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("could not load config: %v", err)
	}
	logger, err := observ.NewLogger(cfg.Env, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()
	srv := gin.New()
	srv.Use(gin.Logger(), gin.Recovery())
	logger.Info("starting EchoStream",
		zap.String("port", cfg.Port),
		zap.String("env", cfg.Env),
	)
	v1 := srv.Group("/v1")
	v1.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})
	srv.Run(":" + cfg.Port)

	return nil
}
