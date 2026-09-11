package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/vitorfulll/android-pessoal/internal/gateway"
	"github.com/vitorfulll/android-pessoal/internal/scrcpy"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadConfig()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := gateway.New(cfg, logger).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (gateway.Config, error) {
	ice := []webrtc.ICEServer{}
	if value := os.Getenv("ANDROID_ICE_SERVERS_JSON"); value != "" {
		if err := json.Unmarshal([]byte(value), &ice); err != nil {
			return gateway.Config{}, err
		}
	}
	return gateway.Config{
		APIBaseURL:    os.Getenv("ANDROID_API_BASE_URL"),
		ExecutorToken: os.Getenv("ANDROID_EXECUTOR_TOKEN"),
		ICEServers:    ice,
		ICEUDPPort:    integer("ANDROID_ICE_UDP_PORT", 25000),
		Scrcpy: scrcpy.Config{
			ADBPath: env("ANDROID_ADB_PATH", "adb"), Serial: os.Getenv("ANDROID_ADB_SERIAL"),
			ServerPath: os.Getenv("ANDROID_SCRCPY_SERVER_PATH"), LocalPort: integer("ANDROID_SCRCPY_PORT", 27183),
			MaxSize: integer("ANDROID_MAX_SIZE", 1280), MaxFPS: integer("ANDROID_MAX_FPS", 60),
			VideoBitrate: integer("ANDROID_VIDEO_BITRATE", 6_000_000), StartupTimeout: 20 * time.Second,
		},
	}, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func integer(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
