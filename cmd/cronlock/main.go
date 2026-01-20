package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cronlock/internal/config"
	"cronlock/internal/lock"
	"cronlock/internal/scheduler"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "cronlock.yaml", "path to configuration file")
	showVersion := flag.Bool("version", false, "show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("cronlock %s\n", version)
		os.Exit(0)
	}

	// Set up structured logging
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Generate node ID if not specified
	nodeID := cfg.Node.ID
	if nodeID == "" {
		hostname, _ := os.Hostname()
		nodeID = fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])
		logger.Info("generated node ID", "node_id", nodeID)
	}

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// Verify Redis connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to Redis", "error", err, "address", cfg.Redis.Address)
		cancel()
		os.Exit(1)
	}
	cancel()
	logger.Info("connected to Redis", "address", cfg.Redis.Address)

	// Create locker
	locker := lock.NewRedisLocker(redisClient, nodeID, cfg.Redis.KeyPrefix)

	// Create scheduler
	sched := scheduler.New(locker, cfg.Node, logger)

	// Add jobs
	for _, jobCfg := range cfg.Jobs {
		if err := sched.AddJob(jobCfg); err != nil {
			logger.Error("failed to add job", "job", jobCfg.Name, "error", err)
			os.Exit(1)
		}
	}

	// Start scheduler
	sched.Start()

	// Notify systemd that we're ready
	notifySystemd(logger)

	// Start systemd watchdog if configured
	stopWatchdog := startWatchdog(logger)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("received shutdown signal", "signal", sig)

	// Stop watchdog
	if stopWatchdog != nil {
		stopWatchdog()
	}

	// Notify systemd we're stopping
	_, _ = daemon.SdNotify(false, daemon.SdNotifyStopping)

	// Stop scheduler gracefully
	sched.Stop()

	// Close locker
	if err := locker.Close(); err != nil {
		logger.Error("failed to close locker", "error", err)
	}

	logger.Info("shutdown complete")
}

// notifySystemd sends the ready notification to systemd if running under systemd.
func notifySystemd(logger *slog.Logger) {
	sent, err := daemon.SdNotify(false, daemon.SdNotifyReady)
	if err != nil {
		logger.Warn("failed to notify systemd", "error", err)
	} else if sent {
		logger.Debug("notified systemd ready")
	}
}

// startWatchdog starts the systemd watchdog if configured.
// Returns a function to stop the watchdog, or nil if not running.
func startWatchdog(logger *slog.Logger) func() {
	interval, err := daemon.SdWatchdogEnabled(false)
	if err != nil || interval == 0 {
		return nil
	}

	logger.Info("starting systemd watchdog", "interval", interval)

	ticker := time.NewTicker(interval / 2)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				ticker.Stop()
				return
			case <-ticker.C:
				_, _ = daemon.SdNotify(false, daemon.SdNotifyWatchdog)
			}
		}
	}()

	return func() {
		close(done)
	}
}
