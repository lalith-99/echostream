package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/api"
	"github.com/lalith-99/echostream/internal/config"
	"github.com/lalith-99/echostream/internal/db"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/observ"
	redisclient "github.com/lalith-99/echostream/internal/redis"
	"github.com/lalith-99/echostream/internal/repository/postgres"
	"github.com/lalith-99/echostream/internal/websocket"
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
		return fmt.Errorf("load config: %w", err)
	}

	logger, err := observ.NewLogger(cfg.Env, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer logger.Sync()

	database, err := db.New(context.Background(), cfg.DatabaseURL, logger)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer database.Close()

	// Redis
	rc, err := redisclient.NewClient(cfg.RedisURL, logger)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer rc.Close()

	// WebSocket hub + Redis pub/sub bridge
	hub := websocket.NewHub(logger)
	pubsub := redisclient.NewPubSub(rc, hub, logger)
	hub.SetChannelCallbacks(pubsub.Subscribe, pubsub.Unsubscribe)

	go hub.Run()
	pubsubCtx, pubsubCancel := context.WithCancel(context.Background())
	defer pubsubCancel()
	go pubsub.Listen(pubsubCtx)
	defer pubsub.Close()

	// Repos
	pool := database.Pool()
	channelRepo := postgres.NewChannelStore(pool)
	membershipRepo := postgres.NewMembershipStore(pool)
	messageRepo := postgres.NewMessageStore(pool)
	userRepo := postgres.NewUserStore(pool)
	tenantRepo := postgres.NewTenantStore(pool)

	// Handlers
	channelHandler := api.NewChannelHandler(channelRepo, logger)
	membershipHandler := api.NewMembershipHandler(membershipRepo, logger)
	messageHandler := api.NewMessageHandler(messageRepo, rc, logger)
	userHandler := api.NewUserHandler(userRepo, logger)
	authHandler := api.NewAuthHandler(userRepo, tenantRepo, cfg.JWTSecret, logger)
	wsHandler := api.NewWSHandler(hub, cfg.JWTSecret, logger)

	srv := gin.New()
	srv.Use(gin.Logger(), gin.Recovery())

	logger.Info("starting EchoStream",
		zap.String("port", cfg.Port),
		zap.String("env", cfg.Env),
	)

	// Public routes
	srv.GET("/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})
	srv.POST("/v1/auth/signup", authHandler.Signup)
	srv.POST("/v1/auth/login", authHandler.Login)
	srv.GET("/v1/ws", wsHandler.HandleWS)

	// Authenticated routes
	v1 := srv.Group("/v1")
	v1.Use(middleware.AuthMiddleware(cfg.JWTSecret))

	v1.POST("/channels", channelHandler.Create)
	v1.GET("/channels", channelHandler.List)
	v1.GET("/channels/:id", channelHandler.GetByID)

	v1.POST("/channels/:id/messages", messageHandler.Create)
	v1.GET("/channels/:id/messages", messageHandler.List)

	v1.POST("/channels/:id/join", membershipHandler.Join)
	v1.POST("/channels/:id/leave", membershipHandler.Leave)
	v1.GET("/channels/:id/members", membershipHandler.ListMembers)

	v1.GET("/users/me", userHandler.GetMe)

	srv.Run(":" + cfg.Port)

	return nil
}
