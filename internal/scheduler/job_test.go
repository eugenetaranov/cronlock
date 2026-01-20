package scheduler

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"cronlock/internal/config"
	"cronlock/internal/executor"
	"cronlock/internal/lock"
)

func newTestJob(cfg config.JobConfig, locker lock.Locker) *Job {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))
	exec := executor.New()
	return NewJob(cfg, locker, exec, 0, logger)
}

func TestNewJob(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:     "test-job",
		Schedule: "* * * * *",
		Command:  "echo hello",
		Timeout:  30 * time.Second,
		LockTTL:  60 * time.Second,
	}

	job := newTestJob(cfg, locker)

	if job == nil {
		t.Fatal("NewJob() returned nil")
	}
	if job.config.Name != "test-job" {
		t.Errorf("job.config.Name = %q, want %q", job.config.Name, "test-job")
	}
	if job.locker != locker {
		t.Error("job.locker not set correctly")
	}
	if job.executor == nil {
		t.Error("job.executor is nil")
	}
}

func TestJob_Run_AcquiresLock(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo hello",
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify lock was acquired
	if len(locker.AcquireCalls) != 1 {
		t.Fatalf("Acquire() called %d times, want 1", len(locker.AcquireCalls))
	}

	call := locker.AcquireCalls[0]
	if call.JobName != "test-job" {
		t.Errorf("Acquire() jobName = %q, want %q", call.JobName, "test-job")
	}
}

func TestJob_Run_LockTTL(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		lockTTL     time.Duration
		expectedTTL time.Duration
	}{
		{
			name:        "explicit lock_ttl",
			timeout:     30 * time.Second,
			lockTTL:     120 * time.Second,
			expectedTTL: 120 * time.Second,
		},
		{
			name:        "default from timeout",
			timeout:     30 * time.Second,
			lockTTL:     0,
			expectedTTL: 30*time.Second + time.Minute,
		},
		{
			name:        "default when no timeout",
			timeout:     0,
			lockTTL:     0,
			expectedTTL: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locker := lock.NewMockLocker()
			cfg := config.JobConfig{
				Name:    "test-job",
				Command: "echo hello",
				Timeout: tt.timeout,
				LockTTL: tt.lockTTL,
			}

			job := newTestJob(cfg, locker)
			job.Run()

			if len(locker.AcquireCalls) != 1 {
				t.Fatalf("Acquire() called %d times, want 1", len(locker.AcquireCalls))
			}

			call := locker.AcquireCalls[0]
			if call.TTL != tt.expectedTTL {
				t.Errorf("Acquire() TTL = %v, want %v", call.TTL, tt.expectedTTL)
			}
		})
	}
}

func TestJob_Run_SkipsIfLockHeld(t *testing.T) {
	locker := lock.NewMockLocker()
	locker.SetLockHeld("test-job", true) // Simulate another node holding the lock

	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo hello",
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Acquire should have been attempted
	if len(locker.AcquireCalls) != 1 {
		t.Errorf("Acquire() called %d times, want 1", len(locker.AcquireCalls))
	}

	// Release should NOT have been called (we didn't get the lock)
	if len(locker.ReleaseCalls) != 0 {
		t.Errorf("Release() called %d times, want 0", len(locker.ReleaseCalls))
	}
}

func TestJob_Run_ReleasesLock(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo hello",
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify lock was released
	if len(locker.ReleaseCalls) != 1 {
		t.Fatalf("Release() called %d times, want 1", len(locker.ReleaseCalls))
	}

	if locker.ReleaseCalls[0] != "test-job" {
		t.Errorf("Release() jobName = %q, want %q", locker.ReleaseCalls[0], "test-job")
	}
}

func TestJob_Run_ExecutesCommand(t *testing.T) {
	locker := lock.NewMockLocker()

	// Create a temp file to verify command execution
	tmpDir := t.TempDir()
	markerFile := tmpDir + "/executed"

	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "touch " + markerFile,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify the command was executed
	if _, err := os.Stat(markerFile); os.IsNotExist(err) {
		t.Error("command was not executed (marker file not created)")
	}
}

func TestJob_Run_SkipsIfAlreadyRunning(t *testing.T) {
	locker := lock.NewMockLocker()

	// Create a job that takes some time
	cfg := config.JobConfig{
		Name:    "long-job",
		Command: "sleep 0.5",
	}

	job := newTestJob(cfg, locker)

	// Start first run in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		job.Run()
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Second run should be skipped
	job.Run()

	wg.Wait()

	// Acquire should only have been called once (second run skipped before acquire)
	if len(locker.AcquireCalls) != 1 {
		t.Errorf("Acquire() called %d times, want 1 (second run should skip)", len(locker.AcquireCalls))
	}
}

func TestJob_IsRunning(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "sleep 0.2",
	}

	job := newTestJob(cfg, locker)

	// Initially not running
	if job.IsRunning() {
		t.Error("IsRunning() = true before Run(), want false")
	}

	// Start in background
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		job.Run()
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Should be running
	if !job.IsRunning() {
		t.Error("IsRunning() = false during Run(), want true")
	}

	wg.Wait()

	// Should not be running after completion
	if job.IsRunning() {
		t.Error("IsRunning() = true after Run(), want false")
	}
}

func TestJob_Cancel(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "long-job",
		Command: "sleep 10",
	}

	job := newTestJob(cfg, locker)

	// Start in background
	var wg sync.WaitGroup
	wg.Add(1)
	start := time.Now()
	go func() {
		defer wg.Done()
		job.Run()
	}()

	// Give it time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the job
	job.Cancel()

	wg.Wait()
	elapsed := time.Since(start)

	// Should have completed quickly (cancelled), not after 10 seconds
	if elapsed > 2*time.Second {
		t.Errorf("Job took %v to complete after cancel, expected much faster", elapsed)
	}
}

func TestJob_Cancel_BeforeRun(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo hello",
	}

	job := newTestJob(cfg, locker)

	// Cancel before run should not panic
	job.Cancel()

	// Job should still run normally
	job.Run()

	if len(locker.AcquireCalls) != 1 {
		t.Errorf("Acquire() called %d times, want 1", len(locker.AcquireCalls))
	}
}

func TestJob_Run_WithWorkDir(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	markerFile := "test-marker"

	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "touch " + markerFile,
		WorkDir: tmpDir,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify the file was created in the work directory
	expectedPath := tmpDir + "/" + markerFile
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("command was not executed in work_dir")
	}
}

func TestJob_Run_WithEnv(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	outputFile := tmpDir + "/output"

	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo $MY_VAR > " + outputFile,
		Env: map[string]string{
			"MY_VAR": "test-value",
		},
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify the env var was set
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if string(content) != "test-value\n" {
		t.Errorf("env var output = %q, want %q", string(content), "test-value\n")
	}
}

func TestJob_Run_AcquireError(t *testing.T) {
	locker := lock.NewMockLocker()
	locker.AcquireError = os.ErrPermission

	tmpDir := t.TempDir()
	markerFile := tmpDir + "/executed"

	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "touch " + markerFile,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Command should not have been executed
	if _, err := os.Stat(markerFile); !os.IsNotExist(err) {
		t.Error("command should not execute when acquire fails")
	}

	// Release should not have been called
	if len(locker.ReleaseCalls) != 0 {
		t.Errorf("Release() called %d times, want 0", len(locker.ReleaseCalls))
	}
}

func TestJob_Run_WithTimeout(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "timeout-job",
		Command: "sleep 10",
		Timeout: 100 * time.Millisecond,
	}

	job := newTestJob(cfg, locker)

	start := time.Now()
	job.Run()
	elapsed := time.Since(start)

	// Should have timed out quickly
	if elapsed > 2*time.Second {
		t.Errorf("job took %v, expected to timeout around 100ms", elapsed)
	}

	// Lock should still be released
	if len(locker.ReleaseCalls) != 1 {
		t.Errorf("Release() called %d times, want 1", len(locker.ReleaseCalls))
	}
}

func TestJob_Run_FailedCommand(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "failing-job",
		Command: "exit 1",
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Lock should still be released even on failure
	if len(locker.ReleaseCalls) != 1 {
		t.Errorf("Release() called %d times, want 1", len(locker.ReleaseCalls))
	}
}

func TestJob_Run_OnSuccessHook(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	hookMarker := tmpDir + "/hook-success"

	cfg := config.JobConfig{
		Name:      "hook-job",
		Command:   "echo success",
		OnSuccess: "touch " + hookMarker,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify success hook was called
	if _, err := os.Stat(hookMarker); os.IsNotExist(err) {
		t.Error("on_success hook was not executed")
	}
}

func TestJob_Run_OnFailureHook(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	hookMarker := tmpDir + "/hook-failure"

	cfg := config.JobConfig{
		Name:      "hook-job",
		Command:   "exit 1",
		OnFailure: "touch " + hookMarker,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Verify failure hook was called
	if _, err := os.Stat(hookMarker); os.IsNotExist(err) {
		t.Error("on_failure hook was not executed")
	}
}

func TestJob_Run_OnSuccessHook_NotCalledOnFailure(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	hookMarker := tmpDir + "/hook-success"

	cfg := config.JobConfig{
		Name:      "failing-job",
		Command:   "exit 1",
		OnSuccess: "touch " + hookMarker,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Success hook should NOT have been called
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Error("on_success hook should not be called on failure")
	}
}

func TestJob_Run_OnFailureHook_NotCalledOnSuccess(t *testing.T) {
	locker := lock.NewMockLocker()
	tmpDir := t.TempDir()
	hookMarker := tmpDir + "/hook-failure"

	cfg := config.JobConfig{
		Name:      "success-job",
		Command:   "echo success",
		OnFailure: "touch " + hookMarker,
	}

	job := newTestJob(cfg, locker)
	job.Run()

	// Failure hook should NOT have been called
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Error("on_failure hook should not be called on success")
	}
}

func TestJob_Run_WithGracePeriod(t *testing.T) {
	locker := lock.NewMockLocker()
	cfg := config.JobConfig{
		Name:    "test-job",
		Command: "echo hello",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	exec := executor.New()
	job := NewJob(cfg, locker, exec, 100*time.Millisecond, logger)

	start := time.Now()
	job.Run()
	elapsed := time.Since(start)

	// Should have waited for grace period
	if elapsed < 100*time.Millisecond {
		t.Errorf("job completed in %v, expected at least 100ms grace period", elapsed)
	}
}
