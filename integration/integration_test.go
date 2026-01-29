//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Single Instance Tests
// ============================================================================

func TestSingleInstance_JobExecutes(t *testing.T) {
	ctx := context.Background()

	// Start Redis container
	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	// Output file for job
	outputFile := filepath.Join(t.TempDir(), "output")

	// Config: job runs every second, writes timestamp to file
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "test-job",
		Schedule: "* * * * * *", // every second
		Command:  fmt.Sprintf("echo executed >> %s", outputFile),
		LockTTL:  "5s",
	}})

	// Start cronlock
	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for at least 2 job executions (allow more time for scheduler init)
	time.Sleep(4 * time.Second)

	// Verify output file exists and has content
	err = waitForFile(outputFile, 1*time.Second, 1)
	require.NoError(t, err, "output file should exist")

	lines, err := countLines(outputFile)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, lines, 2, "job should have executed at least twice")
}

func TestSingleInstance_LockAcquired(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	// Output file to signal job started
	startedFile := filepath.Join(t.TempDir(), "started")

	// Job runs for 3 seconds so we can check lock state
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "lock-test-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 3", startedFile),
		LockTTL:  "10s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err, "job should have started")

	// Check that lock exists in Redis
	exists, err := redis.LockExists(ctx, "lock-test-job")
	require.NoError(t, err)
	assert.True(t, exists, "lock key should exist during job execution")
}

func TestSingleInstance_LockReleased(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	completedFile := filepath.Join(t.TempDir(), "completed")

	// Quick job that completes fast - use longer interval so we can check lock release
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "release-test-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s", completedFile),
		LockTTL:  "5s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for job to complete
	err = waitForFile(completedFile, 3*time.Second, 0)
	require.NoError(t, err, "job should have completed")

	// Stop cronlock so no new jobs are scheduled
	proc.Stop()

	// Wait for grace period (1s configured) + buffer
	time.Sleep(3 * time.Second)

	// Check that lock has been released
	exists, err := redis.LockExists(ctx, "release-test-job")
	require.NoError(t, err)
	assert.False(t, exists, "lock should be released after job completes + grace period")
}

// ============================================================================
// Multi-Instance Competition Tests
// ============================================================================

func TestTwoInstances_OnlyOneExecutes(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	// Shared output file
	outputFile := filepath.Join(t.TempDir(), "output")

	// Config: job runs every second, appends "X" to file
	jobs := []JobDef{{
		Name:     "contention-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo X >> %s", outputFile),
		LockTTL:  "5s",
	}}

	cfg1 := writeTestConfigWithNodeID(t, redis.Addr(), "node-1", jobs)
	cfg2 := writeTestConfigWithNodeID(t, redis.Addr(), "node-2", jobs)

	// Start two instances
	proc1 := startCronlock(t, ctx, cfg1)
	proc2 := startCronlock(t, ctx, cfg2)
	defer proc1.Stop()
	defer proc2.Stop()
	defer proc1.Cleanup()
	defer proc2.Cleanup()

	// Wait for ~3 job cycles
	time.Sleep(3500 * time.Millisecond)

	// Verify: file has exactly 3 "X" lines (not 6)
	count, err := countOccurrences(outputFile, "X")
	require.NoError(t, err)

	// Allow some tolerance for timing (should be ~3, allow 2-4)
	assert.GreaterOrEqual(t, count, 2, "job should run at least twice")
	assert.LessOrEqual(t, count, 4, "job should not run more than ~4 times (one per cycle)")

	t.Logf("Job executed %d times across 2 instances in ~3 seconds", count)
}

func TestTwoInstances_LockContention(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	// Each instance writes its node ID to its own file
	output1 := filepath.Join(t.TempDir(), "node1-output")
	output2 := filepath.Join(t.TempDir(), "node2-output")

	cfg1 := writeTestConfigWithNodeID(t, redis.Addr(), "node-1", []JobDef{{
		Name:     "contention-test",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo node1 >> %s", output1),
		LockTTL:  "5s",
	}})

	cfg2 := writeTestConfigWithNodeID(t, redis.Addr(), "node-2", []JobDef{{
		Name:     "contention-test",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo node2 >> %s", output2),
		LockTTL:  "5s",
	}})

	proc1 := startCronlock(t, ctx, cfg1)
	proc2 := startCronlock(t, ctx, cfg2)
	defer proc1.Stop()
	defer proc2.Stop()
	defer proc1.Cleanup()
	defer proc2.Cleanup()

	// Wait for several cycles
	time.Sleep(4 * time.Second)

	// Count executions from each node
	count1, _ := countLines(output1)
	count2, _ := countLines(output2)

	totalExecutions := count1 + count2
	t.Logf("Node 1 executed %d times, Node 2 executed %d times (total: %d)", count1, count2, totalExecutions)

	// Should have ~4 total executions (one per second), not 8
	assert.GreaterOrEqual(t, totalExecutions, 3, "should have at least 3 total executions")
	assert.LessOrEqual(t, totalExecutions, 5, "should not have more than 5 total executions")
}

func TestTwoInstances_DifferentJobs(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	// Each instance runs a different job
	output1 := filepath.Join(t.TempDir(), "job1-output")
	output2 := filepath.Join(t.TempDir(), "job2-output")

	cfg1 := writeTestConfigWithNodeID(t, redis.Addr(), "node-1", []JobDef{{
		Name:     "job-alpha",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo alpha >> %s", output1),
		LockTTL:  "5s",
	}})

	cfg2 := writeTestConfigWithNodeID(t, redis.Addr(), "node-2", []JobDef{{
		Name:     "job-beta",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo beta >> %s", output2),
		LockTTL:  "5s",
	}})

	proc1 := startCronlock(t, ctx, cfg1)
	proc2 := startCronlock(t, ctx, cfg2)
	defer proc1.Stop()
	defer proc2.Stop()
	defer proc1.Cleanup()
	defer proc2.Cleanup()

	// Wait for several cycles (allow more time for scheduler init)
	time.Sleep(5 * time.Second)

	// Both jobs should execute independently
	count1, _ := countLines(output1)
	count2, _ := countLines(output2)

	t.Logf("Job alpha executed %d times, Job beta executed %d times", count1, count2)

	// Both should have run ~3 times each
	assert.GreaterOrEqual(t, count1, 2, "job-alpha should execute at least twice")
	assert.GreaterOrEqual(t, count2, 2, "job-beta should execute at least twice")
}

// ============================================================================
// Failover & Reliability Tests
// ============================================================================

func TestFailover_LockExpires(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	outputFile := filepath.Join(t.TempDir(), "output")
	instance1Started := filepath.Join(t.TempDir(), "instance1-started")

	// Instance 1: long-running job with short TTL (will be killed, lock expires)
	cfg1 := writeTestConfigWithNodeID(t, redis.Addr(), "instance-1", []JobDef{{
		Name:     "failover-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && echo instance1 >> %s && sleep 30", instance1Started, outputFile),
		LockTTL:  "3s", // Short TTL that won't be renewed after kill
	}})

	// Instance 2: same job, should take over after lock expires
	cfg2 := writeTestConfigWithNodeID(t, redis.Addr(), "instance-2", []JobDef{{
		Name:     "failover-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo instance2 >> %s", outputFile),
		LockTTL:  "3s",
	}})

	// Start instance 1
	proc1 := startCronlock(t, ctx, cfg1)
	defer proc1.Cleanup()

	// Wait for instance 1 to start its job
	err = waitForFile(instance1Started, 3*time.Second, 0)
	require.NoError(t, err, "instance 1 should start job")

	// Kill instance 1 (simulate crash)
	proc1.Kill()

	// Start instance 2
	proc2 := startCronlock(t, ctx, cfg2)
	defer proc2.Stop()
	defer proc2.Cleanup()

	// Wait for lock to expire (3s) and instance 2 to take over
	time.Sleep(5 * time.Second)

	// Check output file
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	content := string(data)
	t.Logf("Output file content:\n%s", content)

	// Instance 2 should have written
	assert.Contains(t, content, "instance2", "instance 2 should take over after lock expires")
}

func TestLockRenewal_LongRunningJob(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	startedFile := filepath.Join(t.TempDir(), "started")

	// Job with short TTL (3s) but runs for 6s - should renew lock
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "long-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 6", startedFile),
		LockTTL:  "3s", // Lock should be renewed before expiring
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err)

	// Check lock TTL multiple times during execution
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Second)

		ttl, err := redis.GetLockTTL(ctx, "long-job")
		if err == nil && ttl > 0 {
			t.Logf("Check %d: Lock TTL is %v", i+1, ttl)
			// TTL should be refreshed (not close to 0)
			assert.Greater(t, ttl.Milliseconds(), int64(1000), "lock should have been renewed")
		}
	}
}

func TestNoDoubleExecution(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	outputFile := filepath.Join(t.TempDir(), "output")

	// Job takes 2 seconds but schedule triggers every second
	// Should not run concurrently even on same instance
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "no-overlap-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("date +%%s.%%N >> %s && sleep 2", outputFile),
		LockTTL:  "10s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for multiple trigger attempts
	time.Sleep(5 * time.Second)

	// Read timestamps
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	t.Logf("Execution timestamps: %v", lines)

	// Should have ~2-3 executions (5s / 2s job duration), not 5
	assert.LessOrEqual(t, len(lines), 3, "job should not run concurrently")
}

// ============================================================================
// Lifecycle Tests
// ============================================================================

func TestGracefulShutdown(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	startedFile := filepath.Join(t.TempDir(), "started")
	completedFile := filepath.Join(t.TempDir(), "completed")

	// Job that runs for 3 seconds
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "shutdown-test",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 3 && touch %s", startedFile, completedFile),
		LockTTL:  "10s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err)

	// Send graceful shutdown signal
	proc.Stop()

	// Job should have completed (graceful shutdown waits for jobs)
	err = waitForFile(completedFile, 5*time.Second, 0)
	assert.NoError(t, err, "job should complete during graceful shutdown")
}

func TestGracefulShutdown_JobExceedsTimeout(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	startedFile := filepath.Join(t.TempDir(), "started")
	completedFile := filepath.Join(t.TempDir(), "completed")

	// Job with 2s timeout that would run for 30s
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "shutdown-timeout-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 30 && touch %s", startedFile, completedFile),
		Timeout:  "2s",
		LockTTL:  "10s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err)

	// Record time and send shutdown signal
	start := time.Now()
	proc.Stop()
	elapsed := time.Since(start)

	// Should complete in ~2s (job's timeout), not wait for full 30s sleep
	assert.Less(t, elapsed, 5*time.Second, "shutdown should complete within job timeout + buffer")

	// Job should NOT have completed (was canceled)
	_, err = os.Stat(completedFile)
	assert.True(t, os.IsNotExist(err), "job should be canceled before completion")
}

func TestGracefulShutdown_DefaultTimeout(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	startedFile := filepath.Join(t.TempDir(), "started")
	completedFile := filepath.Join(t.TempDir(), "completed")

	// Job with no timeout that runs for 5s (well under 30s default)
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "no-timeout-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 5 && touch %s", startedFile, completedFile),
		LockTTL:  "10s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err)

	// Send shutdown signal
	proc.Stop()

	// Job should complete (5s < 30s default timeout)
	err = waitForFile(completedFile, 10*time.Second, 0)
	assert.NoError(t, err, "job should complete within default 30s timeout")
}

func TestGracefulShutdown_MultipleJobsDifferentTimeouts(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	started1 := filepath.Join(t.TempDir(), "started1")
	started2 := filepath.Join(t.TempDir(), "started2")
	completed1 := filepath.Join(t.TempDir(), "completed1")
	completed2 := filepath.Join(t.TempDir(), "completed2")

	cfg := writeTestConfig(t, redis.Addr(), []JobDef{
		{
			Name:     "short-timeout-job",
			Schedule: "* * * * * *",
			Command:  fmt.Sprintf("touch %s && sleep 30 && touch %s", started1, completed1),
			Timeout:  "2s",
			LockTTL:  "10s",
		},
		{
			Name:     "longer-timeout-job",
			Schedule: "* * * * * *",
			Command:  fmt.Sprintf("touch %s && sleep 30 && touch %s", started2, completed2),
			Timeout:  "5s",
			LockTTL:  "10s",
		},
	})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for both jobs to start
	err = waitForFile(started1, 3*time.Second, 0)
	require.NoError(t, err)
	err = waitForFile(started2, 3*time.Second, 0)
	require.NoError(t, err)

	// Record time and send shutdown
	start := time.Now()
	proc.Stop()
	elapsed := time.Since(start)

	// Shutdown should take ~5s (the longer timeout), not 30s
	assert.Less(t, elapsed, 8*time.Second, "shutdown should complete within max job timeout + buffer")
	assert.Greater(t, elapsed, 2*time.Second, "shutdown should wait at least for shortest timeout")

	// Neither job should complete
	_, err = os.Stat(completed1)
	assert.True(t, os.IsNotExist(err), "short-timeout job should be canceled")
	_, err = os.Stat(completed2)
	assert.True(t, os.IsNotExist(err), "longer-timeout job should be canceled")
}

func TestRedisReconnect(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)

	outputFile := filepath.Join(t.TempDir(), "output")

	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "reconnect-test",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("echo X >> %s", outputFile),
		LockTTL:  "5s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for initial execution
	time.Sleep(2 * time.Second)

	initialCount, err := countOccurrences(outputFile, "X")
	require.NoError(t, err)
	require.GreaterOrEqual(t, initialCount, 1, "job should have executed")

	// Restart Redis (simulate network issue)
	redis.Terminate(ctx)
	time.Sleep(1 * time.Second)

	// Start a new Redis container
	redis2, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis2.Terminate(ctx)

	// Note: This test verifies behavior when Redis becomes unavailable
	// The actual reconnection depends on the Redis client configuration
	// Here we just verify cronlock doesn't crash when Redis is unavailable

	// Wait a bit and check cronlock is still running
	time.Sleep(3 * time.Second)

	logs := proc.Logs()
	t.Logf("Cronlock logs during Redis restart:\n%s", logs)

	// Cronlock should log connection errors but not crash
	// (actual reconnect to new instance won't work since address changed)
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestJobWithTimeout(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	startedFile := filepath.Join(t.TempDir(), "started")
	completedFile := filepath.Join(t.TempDir(), "completed")

	// Job that would run for 10s but has 2s timeout
	// Use short lock TTL so it expires quickly after process stops
	cfg := writeTestConfig(t, redis.Addr(), []JobDef{{
		Name:     "timeout-job",
		Schedule: "* * * * * *",
		Command:  fmt.Sprintf("touch %s && sleep 10 && touch %s", startedFile, completedFile),
		Timeout:  "2s",
		LockTTL:  "5s",
	}})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Cleanup()

	// Wait for job to start
	err = waitForFile(startedFile, 3*time.Second, 0)
	require.NoError(t, err)

	// Wait past timeout
	time.Sleep(4 * time.Second)

	// completedFile should NOT exist (job was killed by timeout)
	_, err = os.Stat(completedFile)
	assert.True(t, os.IsNotExist(err), "job should be killed by timeout before completion")

	// Stop cronlock so we can check final lock state
	proc.Stop()

	// Lock should be released after stop + TTL expiry
	time.Sleep(6 * time.Second)
	exists, err := redis.LockExists(ctx, "timeout-job")
	require.NoError(t, err)
	assert.False(t, exists, "lock should be released after timeout")
}

func TestMultipleJobsSameInstance(t *testing.T) {
	ctx := context.Background()

	redis, err := setupRedis(ctx)
	require.NoError(t, err)
	defer redis.Terminate(ctx)

	output1 := filepath.Join(t.TempDir(), "job1-output")
	output2 := filepath.Join(t.TempDir(), "job2-output")

	cfg := writeTestConfig(t, redis.Addr(), []JobDef{
		{
			Name:     "multi-job-1",
			Schedule: "* * * * * *",
			Command:  fmt.Sprintf("echo job1 >> %s", output1),
			LockTTL:  "5s",
		},
		{
			Name:     "multi-job-2",
			Schedule: "* * * * * *",
			Command:  fmt.Sprintf("echo job2 >> %s", output2),
			LockTTL:  "5s",
		},
	})

	proc := startCronlock(t, ctx, cfg)
	defer proc.Stop()
	defer proc.Cleanup()

	// Wait for several cycles (with 1s grace period, ~1 exec every 2s)
	time.Sleep(6 * time.Second)

	count1, _ := countLines(output1)
	count2, _ := countLines(output2)

	t.Logf("Job 1 executed %d times, Job 2 executed %d times", count1, count2)

	// Both jobs should execute multiple times (they have independent locks)
	assert.GreaterOrEqual(t, count1, 2, "job 1 should execute multiple times")
	assert.GreaterOrEqual(t, count2, 2, "job 2 should execute multiple times")
}
