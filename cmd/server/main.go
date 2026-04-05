package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/api"
	"github.com/lalith-99/echostream/internal/config"
	"github.com/lalith-99/echostream/internal/db"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/presence"
	redisclient "github.com/lalith-99/echostream/internal/redis"
	"github.com/lalith-99/echostream/internal/repository/postgres"
	"github.com/lalith-99/echostream/internal/service"
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

	// Presence tracker — marks users online/offline in Redis
	tracker := presence.NewTracker(rc.RDB(), logger)
	hub.SetPresenceTracker(tracker)

	go hub.Run()
	defer hub.Shutdown()

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
	signupRepo := postgres.NewSignupStore(pool)

	// Services (business logic layer)
	messageSvc := service.NewMessageService(messageRepo, membershipRepo, rc, logger)

	// Handlers (thin HTTP adapters)
	channelHandler := api.NewChannelHandler(channelRepo, logger)
	membershipHandler := api.NewMembershipHandler(membershipRepo, channelRepo, logger)
	messageHandler := api.NewMessageHandler(messageSvc, logger)
	userHandler := api.NewUserHandler(userRepo, logger)
	authHandler := api.NewAuthHandler(userRepo, signupRepo, cfg.JWTSecret, logger)
	wsHandler := api.NewWSHandler(hub, membershipRepo, cfg.JWTSecret, logger)

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
	v1.Use(middleware.RateLimiter(rc.RDB(), 60, time.Minute)) // 60 requests/min per user

	v1.POST("/channels", channelHandler.Create)
	v1.GET("/channels", channelHandler.List)
	v1.GET("/channels/:id", channelHandler.GetByID)

	v1.POST("/channels/:id/messages", messageHandler.Create)
	v1.GET("/channels/:id/messages", messageHandler.List)

	v1.POST("/channels/:id/join", membershipHandler.Join)
	v1.POST("/channels/:id/leave", membershipHandler.Leave)
	v1.GET("/channels/:id/members", membershipHandler.ListMembers)

	v1.GET("/users/me", userHandler.GetMe)

	// --- Graceful shutdown ---
	//
	// We use http.Server instead of gin's srv.Run() so we can call
	// Shutdown() when the process receives SIGINT (Ctrl+C) or SIGTERM
	// (Kubernetes pod eviction, docker stop, etc.).
	//
	// Flow:
	//   1. Start HTTP server in a goroutine
	//   2. Block on OS signal
	//   3. Call server.Shutdown (stops accepting new conns, waits for in-flight)
	//   4. Deferred cleanup runs: pubsub → redis → postgres → logger

	httpSrv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: srv,
	}

	// Start serving in background
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	logger.Info("server is ready", zap.String("addr", httpSrv.Addr))

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	logger.Info("shutting down", zap.String("signal", sig.String()))

	// Give in-flight requests 5 seconds to finish
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("forced shutdown", zap.Error(err))
	}

	logger.Info("server stopped cleanly")
	// Deferred cleanup (pubsub, redis, postgres, logger) runs as this function returns.
	return nil
}
