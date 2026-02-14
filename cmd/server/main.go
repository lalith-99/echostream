package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/config"
	"github.com/lalith-99/echostream/internal/db"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/repository/postgres"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// ---------------------------------------------------------------
	// 1. Load config
	// ---------------------------------------------------------------
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// ---------------------------------------------------------------
	// 2. Create logger
	// ---------------------------------------------------------------
	logger, err := observ.NewLogger(cfg.Env, cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}
	defer logger.Sync()

	// ---------------------------------------------------------------
	// 3. Connect to Postgres
	//
	// Why context.Background() here?
	//   - At startup, there's no parent request or deadline.
	//     Background() is the root context — it never cancels.
	//   - Once the server is running, each HTTP request gets its
	//     own context with a deadline. But startup is "take as long
	//     as you need to connect."
	// ---------------------------------------------------------------
	database, err := db.New(context.Background(), cfg.DatabaseURL, logger)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	// defer database.Close() ensures the connection pool is drained
	// when run() returns — whether normally or due to an error.
	// This is the Go cleanup pattern: acquire resource, immediately
	// defer its release.
	defer database.Close()

	// ---------------------------------------------------------------
	// 4. Create repositories
	//
	// Each repo gets the same pool. The pool handles concurrency
	// internally (it's goroutine-safe), so sharing is fine.
	//
	// We assign to the INTERFACE type (repository.XxxRepository),
	// not the concrete type (*postgres.XxxStore). This proves at
	// compile time that our implementations satisfy the interfaces.
	// If ChannelStore is missing a method, this line fails to compile.
	//
	// Using _ here because we're not plugging these into handlers yet
	// — that's the next step. But the wiring is ready.
	// ---------------------------------------------------------------
	pool := database.Pool()
	channelRepo := postgres.NewChannelStore(pool)
	membershipRepo := postgres.NewMembershipStore(pool)
	messageRepo := postgres.NewMessageStore(pool)
	userRepo := postgres.NewUserStore(pool)

	// Temporary: log that repos are ready (proves the wiring works).
	// We'll remove this once handlers are connected.
	logger.Info("repositories initialized",
		zap.String("channel_repo", fmt.Sprintf("%T", channelRepo)),
		zap.String("membership_repo", fmt.Sprintf("%T", membershipRepo)),
		zap.String("message_repo", fmt.Sprintf("%T", messageRepo)),
		zap.String("user_repo", fmt.Sprintf("%T", userRepo)),
	)

	// ---------------------------------------------------------------
	// 5. Set up HTTP server
	// ---------------------------------------------------------------
	srv := gin.New()
	srv.Use(gin.Logger(), gin.Recovery())

	logger.Info("starting EchoStream",
		zap.String("port", cfg.Port),
		zap.String("env", cfg.Env),
	)

	// Health check is PUBLIC — no auth required.
	// Load balancers (ALB, ECS) hit this to check if the server is alive.
	// If it required auth, the LB couldn't health-check us.
	srv.GET("/v1/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// All other /v1/* routes require a valid JWT.
	// The middleware runs BEFORE any handler in this group.
	// If the token is missing/invalid, the request never reaches the handler.
	v1 := srv.Group("/v1")
	v1.Use(middleware.AuthMiddleware(cfg.JWTSecret))

	// Route registration will go here as we build handlers.
	// Example: v1.POST("/channels", channelHandler.Create)
	_ = channelRepo
	_ = membershipRepo
	_ = messageRepo
	_ = userRepo

	srv.Run(":" + cfg.Port)

	return nil
}
