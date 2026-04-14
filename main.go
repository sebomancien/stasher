package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := loadConfig()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})))

	slog.Info("starting", "destination", cfg.BackupDest, "interval", cfg.CheckInterval.String())

	mgr, err := newManager(cfg)
	if err != nil {
		slog.Error("init", "err", err)
		os.Exit(1)
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
