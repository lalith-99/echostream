package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lalith-99/echostream/internal/api"
	"github.com/lalith-99/echostream/internal/config"
	"github.com/lalith-99/echostream/internal/db"
	"github.com/lalith-99/echostream/internal/invite"
	"github.com/lalith-99/echostream/internal/middleware"
	"github.com/lalith-99/echostream/internal/observ"
	"github.com/lalith-99/echostream/internal/presence"
	redisclient "github.com/lalith-99/echostream/internal/redis"
	"github.com/lalith-99/echostream/internal/repository/postgres"
	"github.com/lalith-99/echostream/internal/service"
	"github.com/lalith-99/echostream/internal/websocket"
	"github.com/lalith-99/echostream/migrations"
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
		return err
	}
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	logger, err := observ.NewLogger(cfg.Env, cfg.LogLevel)
	if err != nil {
		return err
	}
	defer logger.Sync()

	startup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stopTracing, err := observ.ConfigureTracing(startup)
	if err != nil {
		return fmt.Errorf("initialize tracing: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := stopTracing(ctx); err != nil {
			logger.Warn("flush traces", zap.Error(err))
		}
	}()
	database, err := db.New(startup, cfg.DatabaseURL, logger)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer database.Close()
	pool := database.Pool()
	if err := migrations.Ready(startup, pool); err != nil {
		return fmt.Errorf("check database schema (run migrations first): %w", err)
	}
	rc, err := redisclient.NewClient(cfg.RedisURL, logger)
	if err != nil {
		return fmt.Errorf("connect to Redis: %w", err)
	}
	defer rc.Close()

	hub := websocket.NewHub(logger)
	pubsub, err := redisclient.NewPubSub(rc, hub, logger)
	if err != nil {
		return err
	}
	tracker := presence.NewTracker(rc.RDB(), logger)
	hub.SetPresenceTracker(tracker)
	channelRepo := postgres.NewChannelStore(pool)
	membershipRepo := postgres.NewMembershipStore(pool)
	messageRepo := postgres.NewMessageStore(pool)
	userRepo := postgres.NewUserStore(pool)
	inviteSvc := invite.NewService(rc.RDB())

	workers, stopWorkers := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	go hub.Run()
	wg.Go(func() { pubsub.Listen(workers) })
	wg.Go(func() { service.RunOutbox(workers, messageRepo, rc, logger) })
	defer func() {
		stopWorkers()
		pubsub.Close()
		wg.Wait()
		hub.Shutdown()
	}()

	channel := api.NewChannelHandler(channelRepo, logger)
	membership := api.NewMembershipHandler(membershipRepo, channelRepo, logger)
	messages := api.NewMessageHandler(service.NewMessageService(messageRepo), logger)
	user := api.NewUserHandler(userRepo, logger)
	auth := api.NewAuthHandler(userRepo, postgres.NewSignupStore(pool), inviteSvc, cfg.JWTSecret, logger)
	ws := api.NewWSHandler(hub, membershipRepo.CheckSocketMembership, cfg.JWTSecret, logger, cfg.CORSAllowedOrigins...)
	pres := api.NewPresenceHandler(channelRepo, membershipRepo, tracker, logger)
	workspace := api.NewWorkspaceHandler(inviteSvc, userRepo, cfg.FrontendBaseURL, logger)

	srv := gin.New()
	if err := srv.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return fmt.Errorf("invalid TRUSTED_PROXY_CIDRS: %w", err)
	}
	metrics := observ.NewHTTPMetrics()
	srv.Use(metrics.Observe(logger), middleware.CORS(cfg.CORSAllowedOrigins), func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		c.Next()
	})
	srv.GET("/v1/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	srv.GET("/v1/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if migrations.Ready(ctx, pool) != nil || rc.RDB().Ping(ctx).Err() != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	authRoutes := srv.Group("/v1/auth", middleware.AuthRateLimiter(rc.RDB(), 30, time.Minute))
	authRoutes.POST("/signup", auth.Signup)
	authRoutes.POST("/login", auth.Login)
	srv.GET("/v1/ws", ws.HandleWS)
	v1 := srv.Group("/v1", middleware.AuthMiddleware(cfg.JWTSecret), middleware.RateLimiter(rc.RDB(), 60, time.Minute))
	v1.POST("/channels", channel.Create)
	v1.GET("/channels", channel.List)
	v1.GET("/channels/:id", channel.GetByID)
	v1.POST("/channels/:id/messages", messages.Create)
	v1.GET("/channels/:id/messages", messages.List)
	v1.POST("/channels/:id/join", membership.Join)
	v1.POST("/channels/:id/leave", membership.Leave)
	v1.POST("/channels/:id/invite", membership.Invite)
	v1.GET("/channels/:id/members", membership.ListMembers)
	v1.GET("/channels/:id/presence", pres.GetChannelPresence)
	v1.GET("/users/me", user.GetMe)
	v1.GET("/users", user.List)
	v1.POST("/workspace/invite", workspace.GenerateInvite)

	servers := []*http.Server{{
		Addr: ":" + cfg.Port, Handler: srv, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 16 << 10,
	}}
	if cfg.AdminPort != "" {
		management := metrics.Management(hub.ConnectionCount, func(ctx context.Context) (int64, float64, error) {
			var pending int64
			var oldest float64
			err := pool.QueryRow(ctx, `SELECT count(*),coalesce(extract(epoch FROM now()-min(enqueued_at)),0) FROM message_outbox`).Scan(&pending, &oldest)
			return pending, oldest, err
		})
		servers = append(servers, &http.Server{Addr: net.JoinHostPort("127.0.0.1", cfg.AdminPort), Handler: management, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second})
	}
	return serve(servers, logger)
}

func serve(servers []*http.Server, logger *zap.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failures := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			logger.Info("starting listener", zap.String("addr", s.Addr))
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				failures <- err
			}
		}()
	}
	var result error
	select {
	case <-ctx.Done():
	case result = <-failures:
	}
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(drain); err != nil {
			_ = s.Close()
			result = errors.Join(result, err)
		}
	}
	return result
}
