package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lalith-99/echostream/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return fmt.Errorf("set DATABASE_URL to the database to upgrade")
	}
	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(stopping, 10*time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("connect to migration database: %w", err)
	}
	defer conn.Close(ctx)
	if err := migrations.Apply(ctx, conn); err != nil {
		return err
	}
	fmt.Println("EchoStream database is up to date")
	return nil
}
