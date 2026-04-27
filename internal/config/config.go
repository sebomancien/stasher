package config

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

// Holds runtime configuration loaded from environment variables.
type Config struct {
	// Directory inside this container where archives are written.
	BackupDest string
	// Controls how often Docker is polled for new/removed containers.
	CheckInterval time.Duration
	// Controls the minimum severity of log messages emitted.
	LogLevel slog.Level
	// TCP address for the web dashboard (e.g. ":8080"). Empty disables it.
	WebAddr string
}

// Loads the configuration from environment variables.
func Load() *Config {
	cfg := Config{
		BackupDest:    "/backups",
		CheckInterval: 60 * time.Second,
		LogLevel:      slog.LevelInfo,
	}

	// Backup destination
	dest, ok := os.LookupEnv("BACKUP_DEST")
	if ok && dest != "" {
		cfg.BackupDest = dest
	}

	// Check interval
	interval, ok := os.LookupEnv("CHECK_INTERVAL")
	if ok && interval != "" {
		v, err := time.ParseDuration(interval)
		if err == nil {
			cfg.CheckInterval = v
		}
	}

	// Log level
	loglevel, ok := os.LookupEnv("LOG_LEVEL")
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

	// Web dashboard address
	cfg.WebAddr = ":8080"
	if addr, ok := os.LookupEnv("WEB_ADDR"); ok {
		cfg.WebAddr = addr
	}

	return &cfg
}
