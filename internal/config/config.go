package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddress     string
	DatabasePath      string
	JWTSecret         []byte
	ExecutorToken     string
	BootstrapUser     string
	BootstrapPassword string
	AccessTTL         time.Duration
	RefreshTTL        time.Duration
	SessionTTL        time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:     env("ANDROID_LISTEN_ADDRESS", "127.0.0.1:18080"),
		DatabasePath:      env("ANDROID_DATABASE_PATH", "data/auth.db"),
		JWTSecret:         []byte(os.Getenv("ANDROID_JWT_SECRET")),
		ExecutorToken:     os.Getenv("ANDROID_EXECUTOR_TOKEN"),
		BootstrapUser:     env("ANDROID_BOOTSTRAP_USER", "vitorfulll"),
		BootstrapPassword: os.Getenv("ANDROID_BOOTSTRAP_PASSWORD"),
		AccessTTL:         minutes("ANDROID_ACCESS_TTL_MINUTES", 10),
		RefreshTTL:        hours("ANDROID_REFRESH_TTL_HOURS", 24*30),
		SessionTTL:        hours("ANDROID_SESSION_TTL_HOURS", 12),
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("ANDROID_JWT_SECRET must contain at least 32 bytes")
	}
	if len(cfg.ExecutorToken) < 32 {
		return Config{}, errors.New("ANDROID_EXECUTOR_TOKEN must contain at least 32 bytes")
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func minutes(name string, fallback int) time.Duration {
	return time.Duration(integer(name, fallback)) * time.Minute
}

func hours(name string, fallback int) time.Duration {
	return time.Duration(integer(name, fallback)) * time.Hour
}

func integer(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
