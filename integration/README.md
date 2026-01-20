# Integration Tests

## Overview

Integration tests verify cronlock's distributed locking behavior in realistic scenarios using actual Redis instances. These tests ensure that:

- Jobs execute correctly on schedule
- Distributed locks prevent concurrent execution across multiple instances
- Lock acquisition, renewal, and release work properly
- Failover occurs correctly when instances crash
- Graceful shutdown completes running jobs

## Prerequisites

- **Docker**: Required for [testcontainers-go](https://golang.testcontainers.org/) to spin up Redis containers
- Docker daemon must be running before executing tests

## Running Tests

```bash
# Run integration tests only
make test-integration

# Run all tests (unit + integration)
make test-all
```

The integration tests use the `-tags=integration` build tag and have a 5-minute timeout.

## Test Categories

### Single Instance Tests

Tests that verify basic functionality with a single cronlock instance.

| Test | Description |
|------|-------------|
| `TestSingleInstance_JobExecutes` | Verifies that a scheduled job executes multiple times on its cron schedule |
| `TestSingleInstance_LockAcquired` | Confirms that a lock key exists in Redis while a job is running |
| `TestSingleInstance_LockReleased` | Verifies that the lock is released after the job completes and grace period expires |

### Multi-Instance Competition Tests

Tests that verify distributed locking behavior when multiple cronlock instances compete for the same jobs.

| Test | Description |
|------|-------------|
| `TestTwoInstances_OnlyOneExecutes` | Two instances with the same job - only one should execute per schedule tick |
| `TestTwoInstances_LockContention` | Tracks which instance wins the lock over multiple cycles, ensuring no double execution |
| `TestTwoInstances_DifferentJobs` | Two instances running different jobs should both execute independently (no interference) |

### Failover & Reliability Tests

Tests that verify behavior during failures and edge conditions.

| Test | Description |
|------|-------------|
| `TestFailover_LockExpires` | When instance 1 is killed mid-job, instance 2 takes over after the lock TTL expires |
| `TestLockRenewal_LongRunningJob` | A job running longer than its lock TTL should have its lock renewed automatically |
| `TestNoDoubleExecution` | A job that runs longer than its schedule interval should not overlap with itself |

### Lifecycle Tests

Tests that verify process lifecycle behavior.

| Test | Description |
|------|-------------|
| `TestGracefulShutdown` | Sending SIGTERM allows running jobs to complete before the process exits |
| `TestRedisReconnect` | Cronlock continues running (doesn't crash) when Redis becomes temporarily unavailable |

### Edge Case Tests

Tests for specific edge conditions and configurations.

| Test | Description |
|------|-------------|
| `TestJobWithTimeout` | A job exceeding its configured timeout is killed before completion |
| `TestMultipleJobsSameInstance` | Multiple jobs on the same instance execute independently with separate locks |

## Test Helpers

The `helpers_test.go` file provides utilities for integration tests:

- **`RedisContainer`**: Wrapper around testcontainers Redis instance with methods for checking lock state
- **`setupRedis`**: Starts a Redis 7 Alpine container and returns connection info
- **`writeTestConfig` / `writeTestConfigWithNodeID`**: Generate YAML config files for test scenarios
- **`CronlockProcess`**: Wrapper for managing cronlock subprocesses (start, stop, kill, logs)
- **`startCronlock`**: Builds and starts a cronlock instance with the given config
- **`waitForFile`**: Polls for a file to exist with optional minimum size
- **`countLines` / `countOccurrences`**: Utilities for verifying job output

## Notes

- **`TestRedisReconnect`**: This test intentionally terminates Redis and starts a new instance. You will see connection error logs from cronlock during the Redis outage period - this is expected behavior. The test verifies that cronlock doesn't crash when Redis is unavailable.

- **Timing tolerance**: Many tests allow timing flexibility (e.g., expecting 2-4 executions instead of exactly 3) to account for scheduler initialization time and system load variations.

- **Grace period**: Tests configure a 1-second grace period, which affects how quickly locks are released after job completion.
