package scheduler

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"cronlock/internal/config"
	"cronlock/internal/lock"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))
}

func TestNew(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		ID:          "test-node",
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()

	s := New(locker, nodeCfg, logger)

	if s == nil {
		t.Fatal("New() returned nil")
	}
	if s.locker != locker {
		t.Error("scheduler locker not set correctly")
	}
	if s.executor == nil {
		t.Error("scheduler executor is nil")
	}
	if s.cron == nil {
		t.Error("scheduler cron is nil")
	}
	if s.jobs == nil {
		t.Error("scheduler jobs map is nil")
	}
}

func TestAddJob(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	jobCfg := config.JobConfig{
		Name:     "test-job",
		Schedule: "* * * * *",
		Command:  "echo test",
	}

	err := s.AddJob(jobCfg)
	if err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	// Verify job was added
	job, ok := s.GetJob("test-job")
	if !ok {
		t.Error("GetJob() returned false, want true")
	}
	if job == nil {
		t.Error("GetJob() returned nil job")
	}

	// Verify cron entry was created
	entries := s.Entries()
	if len(entries) != 1 {
		t.Errorf("len(Entries()) = %d, want 1", len(entries))
	}
}

func TestAddJob_MultipleJobs(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	jobs := []config.JobConfig{
		{Name: "job1", Schedule: "* * * * *", Command: "echo 1"},
		{Name: "job2", Schedule: "*/5 * * * *", Command: "echo 2"},
		{Name: "job3", Schedule: "0 * * * *", Command: "echo 3"},
	}

	for _, jobCfg := range jobs {
		if err := s.AddJob(jobCfg); err != nil {
			t.Fatalf("AddJob(%s) error = %v", jobCfg.Name, err)
		}
	}

	// Verify all jobs were added
	allJobs := s.Jobs()
	if len(allJobs) != 3 {
		t.Errorf("len(Jobs()) = %d, want 3", len(allJobs))
	}

	for _, name := range []string{"job1", "job2", "job3"} {
		if _, ok := allJobs[name]; !ok {
			t.Errorf("job %q not found in Jobs()", name)
		}
	}

	// Verify cron entries
	entries := s.Entries()
	if len(entries) != 3 {
		t.Errorf("len(Entries()) = %d, want 3", len(entries))
	}
}

func TestAddJob_Disabled(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	enabled := false
	jobCfg := config.JobConfig{
		Name:     "disabled-job",
		Schedule: "* * * * *",
		Command:  "echo disabled",
		Enabled:  &enabled,
	}

	err := s.AddJob(jobCfg)
	if err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	// Disabled job should not be added
	_, ok := s.GetJob("disabled-job")
	if ok {
		t.Error("disabled job should not be added to scheduler")
	}

	entries := s.Entries()
	if len(entries) != 0 {
		t.Errorf("len(Entries()) = %d, want 0 (disabled job)", len(entries))
	}
}

func TestAddJob_EnabledNil(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	// Nil Enabled should default to true
	jobCfg := config.JobConfig{
		Name:     "default-enabled-job",
		Schedule: "* * * * *",
		Command:  "echo enabled",
		Enabled:  nil,
	}

	err := s.AddJob(jobCfg)
	if err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	_, ok := s.GetJob("default-enabled-job")
	if !ok {
		t.Error("job with nil Enabled should be added (defaults to true)")
	}
}

func TestAddJob_EnabledTrue(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	enabled := true
	jobCfg := config.JobConfig{
		Name:     "enabled-job",
		Schedule: "* * * * *",
		Command:  "echo enabled",
		Enabled:  &enabled,
	}

	err := s.AddJob(jobCfg)
	if err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	_, ok := s.GetJob("enabled-job")
	if !ok {
		t.Error("enabled job should be added")
	}
}

func TestAddJob_InvalidSchedule(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	jobCfg := config.JobConfig{
		Name:     "invalid-schedule-job",
		Schedule: "not a valid cron",
		Command:  "echo test",
	}

	err := s.AddJob(jobCfg)
	if err == nil {
		t.Error("AddJob() with invalid schedule should return error")
	}
}

func TestAddJob_InvalidScheduleVariants(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()

	invalidSchedules := []string{
		"",
		"invalid",
		"* * *",      // too few fields
		"60 * * * *", // invalid minute
		"* 25 * * *", // invalid hour
	}

	for _, schedule := range invalidSchedules {
		t.Run(schedule, func(t *testing.T) {
			s := New(locker, nodeCfg, logger)

			jobCfg := config.JobConfig{
				Name:     "test-job",
				Schedule: schedule,
				Command:  "echo test",
			}

			err := s.AddJob(jobCfg)
			if err == nil {
				t.Errorf("AddJob() with schedule %q should return error", schedule)
			}
		})
	}
}

func TestAddJob_ValidScheduleVariants(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()

	validSchedules := []string{
		"* * * * *",          // every minute
		"*/5 * * * *",        // every 5 minutes
		"0 * * * *",          // every hour
		"0 0 * * *",          // every day at midnight
		"0 0 * * 0",          // every Sunday at midnight
		"@hourly",            // cron descriptor
		"@daily",             // cron descriptor
		"@weekly",            // cron descriptor
		"30 * * * * *",       // with optional seconds
		"0 30 * * * *",       // with optional seconds
	}

	for _, schedule := range validSchedules {
		t.Run(schedule, func(t *testing.T) {
			s := New(locker, nodeCfg, logger)

			jobCfg := config.JobConfig{
				Name:     "test-job",
				Schedule: schedule,
				Command:  "echo test",
			}

			err := s.AddJob(jobCfg)
			if err != nil {
				t.Errorf("AddJob() with valid schedule %q returned error: %v", schedule, err)
			}
		})
	}
}

func TestGetJob(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	// Add a job
	jobCfg := config.JobConfig{
		Name:     "my-job",
		Schedule: "* * * * *",
		Command:  "echo test",
	}
	s.AddJob(jobCfg)

	// Get existing job
	job, ok := s.GetJob("my-job")
	if !ok {
		t.Error("GetJob() returned false for existing job")
	}
	if job == nil {
		t.Error("GetJob() returned nil for existing job")
	}

	// Get non-existent job
	job, ok = s.GetJob("nonexistent")
	if ok {
		t.Error("GetJob() returned true for non-existent job")
	}
	if job != nil {
		t.Error("GetJob() returned non-nil for non-existent job")
	}
}

func TestJobs(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	// Initially empty
	jobs := s.Jobs()
	if len(jobs) != 0 {
		t.Errorf("Jobs() on empty scheduler = %d jobs, want 0", len(jobs))
	}

	// Add jobs
	for _, name := range []string{"job-a", "job-b", "job-c"} {
		s.AddJob(config.JobConfig{
			Name:     name,
			Schedule: "* * * * *",
			Command:  "echo " + name,
		})
	}

	jobs = s.Jobs()
	if len(jobs) != 3 {
		t.Errorf("Jobs() = %d jobs, want 3", len(jobs))
	}

	// Verify it's a copy (not the internal map)
	delete(jobs, "job-a")
	internalJobs := s.Jobs()
	if len(internalJobs) != 3 {
		t.Error("Jobs() should return a copy, not the internal map")
	}
}

func TestScheduler_StartStop(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	// Add a job
	s.AddJob(config.JobConfig{
		Name:     "test",
		Schedule: "* * * * *",
		Command:  "echo test",
	})

	// Start and stop should not panic
	s.Start()
	s.Stop()
}

func TestScheduler_Entries(t *testing.T) {
	locker := lock.NewMockLocker()
	nodeCfg := config.NodeConfig{
		GracePeriod: 5 * time.Second,
	}
	logger := newTestLogger()
	s := New(locker, nodeCfg, logger)

	// Initially empty
	entries := s.Entries()
	if len(entries) != 0 {
		t.Errorf("Entries() on empty scheduler = %d, want 0", len(entries))
	}

	// Add jobs and check entries
	s.AddJob(config.JobConfig{Name: "job1", Schedule: "* * * * *", Command: "echo 1"})
	s.AddJob(config.JobConfig{Name: "job2", Schedule: "*/5 * * * *", Command: "echo 2"})

	entries = s.Entries()
	if len(entries) != 2 {
		t.Errorf("Entries() = %d, want 2", len(entries))
	}
}
