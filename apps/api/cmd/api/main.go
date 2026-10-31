package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/config"
	"github.com/haseeb-arshad/ride-hailing/apps/api/internal/persistence"
	httptransport "github.com/haseeb-arshad/ride-hailing/apps/api/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	var pool *persistence.Pool
	if cfg.DatabaseURL != "" {
		pool, err = persistence.NewPool(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
	} else {
		logger.Warn("database is not configured; readiness will remain unavailable")
	}

	handler := httptransport.NewHandler(pool, cfg.RequestTimeout, cfg.AppOrigin, cfg.SessionCookieSecure, logger)
	server := &http.Server{
		Addr:              net.JoinHostPort("127.0.0.1", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return err
		}
		logger.Info("api shutdown complete")
		return nil
	}
}
