package main

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	ENV_BACKUP_DEST    = "BACKUP_DEST"
	ENV_CHECK_INTERVAL = "CHECK_INTERVAL"
	ENV_LOG_LEVEL      = "LOG_LEVEL"
)

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	// BackupDest is the directory inside this container where archives are written.
	BackupDest string
	// CheckInterval controls how often Docker is polled for new/removed containers.
	CheckInterval time.Duration
	// LogLevel controls the minimum severity of log messages emitted.
	LogLevel slog.Level
}

var DEFAULT_CONFIG = Config{
	BackupDest:    "/backups",
	CheckInterval: 60 * time.Second,
	LogLevel:      slog.LevelInfo,
}

func loadConfig() Config {
	cfg := DEFAULT_CONFIG

	// Backup destination
	dest, ok := os.LookupEnv(ENV_BACKUP_DEST)
	if ok && dest != "" {
		cfg.BackupDest = dest
	}

	// Check interval
	interval, ok := os.LookupEnv(ENV_CHECK_INTERVAL)
	if ok && interval != "" {
		v, err := time.ParseDuration(interval)
		if err == nil {
			cfg.CheckInterval = v
		}
	}

	// Log level
	loglevel, ok := os.LookupEnv(ENV_LOG_LEVEL)
	if ok {
		switch strings.ToLower(loglevel) {
		case "error":
			cfg.LogLevel = slog.LevelError
		case "warning":
			cfg.LogLevel = slog.LevelWarn
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "debug", "verbose":
			cfg.LogLevel = slog.LevelDebug
		}
	}

	return cfg
}
