package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vitorfulll/android-pessoal/internal/config"
	"github.com/vitorfulll/android-pessoal/internal/httpapi"
	"github.com/vitorfulll/android-pessoal/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	database, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	service := httpapi.New(cfg, database, logger)
	if err := service.Bootstrap(context.Background()); err != nil {
		logger.Error("bootstrap", "error", err)
		os.Exit(1)
	}
	httpServer := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           service.Handler(),
		ReadHeaderTimeout: 8 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		logger.Info("api listening", "address", cfg.ListenAddress)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err)
			os.Exit(1)
		}
	}()
	stop, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
	ctx, shutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdown()
	_ = httpServer.Shutdown(ctx)
}
