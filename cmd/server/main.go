package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"questionnaire/internal/store"
	"questionnaire/internal/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	db, err := store.New(ctx, env("MONGO_URI", "mongodb://127.0.0.1:27018/?directConnection=true"), env("MONGO_DB", "questionnaire_lab1"))
	cancel()
	if err != nil {
		slog.Error("MongoDB initialization failed (check URI, authentication and database permissions)", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Close(ctx)
	}()
	server := &http.Server{Addr: env("LISTEN_ADDR", "127.0.0.1") + ":" + env("PORT", "8080"), Handler: web.New(db, os.Getenv("COOKIE_SECURE") == "true"), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	slog.Info("Survey Lab listening", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("HTTP server failed", "error", err)
		os.Exit(1)
	}
}
