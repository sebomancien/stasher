package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"stasher/internal/config"
	"syscall"
)

func main() {
	cfg := config.Load()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})))

	slog.Info("starting", "destination", cfg.BackupDest, "interval", cfg.CheckInterval.String())

	mgr, err := newManager(cfg)
	if err != nil {
		slog.Error("init", "err", err)
		os.Exit(1)
	}

	if cfg.WebAddr != "" {
		srv := &http.Server{Addr: cfg.WebAddr, Handler: http.HandlerFunc(mgr.dashboardHandler)}
		go func() {
			slog.Info("web dashboard", "addr", cfg.WebAddr)
			err := srv.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				slog.Error("web server", "err", err)
			}
		}()
		defer srv.Shutdown(context.Background())
	}

	ctx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigs
		slog.Info("shutting down", "signal", s)
		cancel()
	}()

	err = mgr.Run(ctx)
	if err != nil && err != context.Canceled {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
