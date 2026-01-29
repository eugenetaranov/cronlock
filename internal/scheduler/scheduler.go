package scheduler

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"cronlock/internal/config"
	"cronlock/internal/executor"
	"cronlock/internal/lock"

	"github.com/robfig/cron/v3"
)

const defaultShutdownTimeout = 30 * time.Second

// Scheduler manages cron job scheduling with distributed locking.
type Scheduler struct {
	cron        *cron.Cron
	locker      lock.Locker
	executor    *executor.Executor
	gracePeriod config.NodeConfig
	logger      *slog.Logger

	mu   sync.Mutex
	jobs map[string]*Job
}

// New creates a new Scheduler.
func New(locker lock.Locker, nodeCfg config.NodeConfig, logger *slog.Logger) *Scheduler {
	// Create cron with seconds field support (optional) and standard parser
	c := cron.New(cron.WithParser(cron.NewParser(
		cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)))

	return &Scheduler{
		cron:        c,
		locker:      locker,
		executor:    executor.New(),
		gracePeriod: nodeCfg,
		logger:      logger,
		jobs:        make(map[string]*Job),
	}
}

// AddJob adds a job to the scheduler.
func (s *Scheduler) AddJob(cfg config.JobConfig) error {
	if !cfg.IsEnabled() {
		s.logger.Info("job is disabled, skipping", "job", cfg.Name)
		return nil
	}

	job := NewJob(cfg, s.locker, s.executor, s.gracePeriod.GracePeriod, s.logger)

	entryID, err := s.cron.AddJob(cfg.Schedule, job)
	if err != nil {
		return fmt.Errorf("failed to add job %s: %w", cfg.Name, err)
	}

	s.mu.Lock()
	s.jobs[cfg.Name] = job
	s.mu.Unlock()

	s.logger.Info("added job",
		"job", cfg.Name,
		"schedule", cfg.Schedule,
		"entry_id", entryID,
	)

	return nil
}

// Start starts the scheduler.
func (s *Scheduler) Start() {
	s.logger.Info("starting scheduler", "job_count", len(s.jobs))
	s.cron.Start()
}

// Stop stops the scheduler and waits for running jobs to complete.
// Each job is given up to its configured timeout to finish.
// Jobs without a timeout default to 30 seconds.
func (s *Scheduler) Stop() {
	s.logger.Info("stopping scheduler")

	// Stop accepting new jobs
	s.cron.Stop()

	// Get currently running jobs
	s.mu.Lock()
	var runningJobs []*Job
	for _, job := range s.jobs {
		if job.IsRunning() {
			runningJobs = append(runningJobs, job)
		}
	}
	s.mu.Unlock()

	if len(runningJobs) == 0 {
		s.logger.Info("no running jobs, scheduler stopped")
		return
	}

	s.logger.Info("waiting for running jobs to complete", "count", len(runningJobs))

	// Wait for each job with its timeout
	var wg sync.WaitGroup
	for _, job := range runningJobs {
		wg.Add(1)
		go func(j *Job) {
			defer wg.Done()
			s.waitForJobWithTimeout(j)
		}(job)
	}

	wg.Wait()
	s.logger.Info("scheduler stopped")
}

// waitForJobWithTimeout waits for a job to complete, canceling it if it exceeds its timeout.
// Jobs without a configured timeout use defaultShutdownTimeout (30s).
func (s *Scheduler) waitForJobWithTimeout(job *Job) {
	timeout := job.Timeout()
	if timeout == 0 {
		timeout = defaultShutdownTimeout
	}

	// Poll for job completion with timeout
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			s.logger.Warn("job exceeded shutdown timeout, canceling",
				"job", job.Name(),
				"timeout", timeout,
			)
			job.Cancel()
			return
		case <-ticker.C:
			if !job.IsRunning() {
				s.logger.Info("job completed during shutdown", "job", job.Name())
				return
			}
		}
	}
}

// GetJob returns a job by name.
func (s *Scheduler) GetJob(name string) (*Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[name]
	return job, ok
}

// Jobs returns all registered jobs.
func (s *Scheduler) Jobs() map[string]*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]*Job, len(s.jobs))
	for k, v := range s.jobs {
		result[k] = v
	}
	return result
}

// Entries returns the cron entries for inspection.
func (s *Scheduler) Entries() []cron.Entry {
	return s.cron.Entries()
}
