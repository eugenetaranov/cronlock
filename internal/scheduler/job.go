package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cronlock/internal/config"
	"cronlock/internal/executor"
	"cronlock/internal/lock"
)

// formatDuration formats a duration as seconds with 2 decimal places.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// Job represents a scheduled job with distributed locking.
type Job struct {
	config      config.JobConfig
	locker      lock.Locker
	executor    *executor.Executor
	gracePeriod time.Duration
	logger      *slog.Logger

	mu        sync.Mutex
	running   bool
	cancelCtx context.CancelFunc
}

// NewJob creates a new Job instance.
func NewJob(cfg config.JobConfig, locker lock.Locker, exec *executor.Executor, gracePeriod time.Duration, logger *slog.Logger) *Job {
	return &Job{
		config:      cfg,
		locker:      locker,
		executor:    exec,
		gracePeriod: gracePeriod,
		logger:      logger.With("job", cfg.Name),
	}
}

// Run executes the job with distributed locking.
// This method is called by the cron scheduler.
func (j *Job) Run() {
	j.mu.Lock()
	if j.running {
		j.logger.Warn("job is already running, skipping")
		j.mu.Unlock()
		return
	}
	j.running = true
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.running = false
		j.mu.Unlock()
	}()

	ctx := context.Background()

	// Determine lock TTL
	lockTTL := j.config.LockTTL
	if lockTTL == 0 {
		// Default to timeout + 1 minute, or 5 minutes if no timeout
		if j.config.Timeout > 0 {
			lockTTL = j.config.Timeout + time.Minute
		} else {
			lockTTL = 5 * time.Minute
		}
	}

	// Try to acquire the lock
	acquired, err := j.locker.Acquire(ctx, j.config.Name, lockTTL)
	if err != nil {
		j.logger.Error("failed to acquire lock", "error", err)
		return
	}
	if !acquired {
		j.logger.Debug("lock not acquired, another node is executing")
		return
	}

	j.logger.Info("acquired lock, starting execution")

	// Create cancellable context for execution
	execCtx, cancel := context.WithCancel(ctx)
	j.mu.Lock()
	j.cancelCtx = cancel
	j.mu.Unlock()

	// Apply timeout if configured
	if j.config.Timeout > 0 {
		var timeoutCancel context.CancelFunc
		execCtx, timeoutCancel = context.WithTimeout(execCtx, j.config.Timeout)
		defer timeoutCancel()
	}

	// Start lock renewal goroutine
	renewDone := make(chan struct{})
	go j.renewLock(ctx, lockTTL, renewDone)

	// Execute the command
	result := j.executor.Execute(execCtx, executor.Options{
		Command: j.config.Command,
		WorkDir: j.config.WorkDir,
		Env:     j.config.Env,
		Timeout: j.config.Timeout,
	})

	// Stop lock renewal
	close(renewDone)

	// Log result
	if result.Success() {
		j.logger.Info("job completed successfully",
			"duration", formatDuration(result.Duration),
			"exit_code", result.ExitCode,
		)
		// Run success hook if configured
		if j.config.OnSuccess != "" {
			j.runHook(ctx, j.config.OnSuccess, "success")
		}
	} else {
		j.logger.Error("job failed",
			"duration", formatDuration(result.Duration),
			"exit_code", result.ExitCode,
			"error", result.Err,
			"stderr", result.Stderr,
		)
		// Run failure hook if configured
		if j.config.OnFailure != "" {
			j.runHook(ctx, j.config.OnFailure, "failure")
		}
	}

	// Wait grace period before releasing lock
	if j.gracePeriod > 0 {
		j.logger.Debug("waiting grace period before releasing lock", "duration", formatDuration(j.gracePeriod))
		time.Sleep(j.gracePeriod)
	}

	// Release the lock
	if err := j.locker.Release(ctx, j.config.Name); err != nil {
		j.logger.Error("failed to release lock", "error", err)
	} else {
		j.logger.Debug("released lock")
	}
}

// renewLock periodically extends the lock TTL while the job is running.
func (j *Job) renewLock(ctx context.Context, ttl time.Duration, done <-chan struct{}) {
	// Renew every TTL/3
	interval := ttl / 3
	if interval < time.Second {
		interval = time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			extended, err := j.locker.Extend(ctx, j.config.Name, ttl)
			if err != nil {
				j.logger.Error("failed to extend lock", "error", err)
			} else if !extended {
				j.logger.Warn("lock extension failed, lock may have been lost")
			} else {
				j.logger.Debug("extended lock", "ttl", ttl)
			}
		}
	}
}

// runHook executes a hook command (on_success or on_failure).
func (j *Job) runHook(ctx context.Context, command, hookType string) {
	j.logger.Debug("running hook", "type", hookType, "command", command)

	result := j.executor.Execute(ctx, executor.Options{
		Command: command,
		WorkDir: j.config.WorkDir,
		Env:     j.config.Env,
	})

	if !result.Success() {
		j.logger.Warn("hook failed",
			"type", hookType,
			"exit_code", result.ExitCode,
			"error", result.Err,
		)
	}
}

// Cancel requests cancellation of the running job.
func (j *Job) Cancel() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cancelCtx != nil {
		j.cancelCtx()
	}
}

// IsRunning returns whether the job is currently executing.
func (j *Job) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// Timeout returns the job's configured timeout.
func (j *Job) Timeout() time.Duration {
	return j.config.Timeout
}

// Name returns the job's name.
func (j *Job) Name() string {
	return j.config.Name
}
