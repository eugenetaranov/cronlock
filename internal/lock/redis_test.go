package lock

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMiniredis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	t.Cleanup(func() {
		client.Close()
		s.Close()
	})

	return s, client
}

func TestRedisLocker_Acquire(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()
	acquired, err := locker.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Error("Acquire() = false, want true")
	}

	// Verify lock exists in Redis
	key := locker.lockKey("test-job")
	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists != 1 {
		t.Error("lock key should exist in Redis")
	}

	// Verify TTL is set
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("TTL() error = %v", err)
	}
	if ttl <= 0 {
		t.Errorf("TTL = %v, want > 0", ttl)
	}
}

func TestRedisLocker_Acquire_AlreadyHeld(t *testing.T) {
	_, client := setupMiniredis(t)

	locker1 := NewRedisLocker(client, "node-1", "test:")
	locker2 := NewRedisLocker(client, "node-2", "test:")

	ctx := context.Background()

	// First locker acquires
	acquired1, err := locker1.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired1 {
		t.Error("first Acquire() = false, want true")
	}

	// Second locker should fail
	acquired2, err := locker2.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if acquired2 {
		t.Error("second Acquire() = true, want false (lock already held)")
	}
}

func TestRedisLocker_Acquire_DifferentJobs(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Acquire locks for different jobs
	acquired1, err := locker.Acquire(ctx, "job-1", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire(job-1) error = %v", err)
	}
	if !acquired1 {
		t.Error("Acquire(job-1) = false, want true")
	}

	acquired2, err := locker.Acquire(ctx, "job-2", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire(job-2) error = %v", err)
	}
	if !acquired2 {
		t.Error("Acquire(job-2) = false, want true")
	}
}

func TestRedisLocker_Release(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Acquire the lock
	acquired, err := locker.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("Acquire() = false, want true")
	}

	// Release the lock
	err = locker.Release(ctx, "test-job")
	if err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Verify lock is removed from Redis
	key := locker.lockKey("test-job")
	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists != 0 {
		t.Error("lock key should not exist after release")
	}
}

func TestRedisLocker_Release_NotOwned(t *testing.T) {
	_, client := setupMiniredis(t)

	locker1 := NewRedisLocker(client, "node-1", "test:")
	locker2 := NewRedisLocker(client, "node-2", "test:")

	ctx := context.Background()

	// First locker acquires
	acquired, err := locker1.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("Acquire() = false, want true")
	}

	// Second locker tries to release (should be no-op)
	err = locker2.Release(ctx, "test-job")
	if err != nil {
		t.Fatalf("Release() error = %v, want nil", err)
	}

	// Lock should still exist
	key := locker1.lockKey("test-job")
	exists, err := client.Exists(ctx, key).Result()
	if err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
	if exists != 1 {
		t.Error("lock key should still exist (not owned by releaser)")
	}
}

func TestRedisLocker_Release_NeverAcquired(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Release without acquiring should be no-op
	err := locker.Release(ctx, "never-acquired")
	if err != nil {
		t.Fatalf("Release() error = %v, want nil", err)
	}
}

func TestRedisLocker_Extend(t *testing.T) {
	s, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Acquire the lock with short TTL
	acquired, err := locker.Acquire(ctx, "test-job", 10*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("Acquire() = false, want true")
	}

	// Get initial TTL
	key := locker.lockKey("test-job")
	initialTTL, _ := client.TTL(ctx, key).Result()

	// Fast-forward time a bit
	s.FastForward(5 * time.Second)

	// Extend the lock
	extended, err := locker.Extend(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Extend() error = %v", err)
	}
	if !extended {
		t.Error("Extend() = false, want true")
	}

	// Verify TTL was extended
	newTTL, _ := client.TTL(ctx, key).Result()
	if newTTL <= initialTTL-5*time.Second {
		t.Errorf("TTL should be extended, got %v", newTTL)
	}
}

func TestRedisLocker_Extend_NotOwned(t *testing.T) {
	_, client := setupMiniredis(t)

	locker1 := NewRedisLocker(client, "node-1", "test:")
	locker2 := NewRedisLocker(client, "node-2", "test:")

	ctx := context.Background()

	// First locker acquires
	acquired, err := locker1.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !acquired {
		t.Fatal("Acquire() = false, want true")
	}

	// Second locker tries to extend (should fail)
	extended, err := locker2.Extend(ctx, "test-job", 60*time.Second)
	if err != nil {
		t.Fatalf("Extend() error = %v, want nil", err)
	}
	if extended {
		t.Error("Extend() = true, want false (not owner)")
	}
}

func TestRedisLocker_Extend_NeverAcquired(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Extend without acquiring should return false
	extended, err := locker.Extend(ctx, "never-acquired", 30*time.Second)
	if err != nil {
		t.Fatalf("Extend() error = %v, want nil", err)
	}
	if extended {
		t.Error("Extend() = true, want false (never acquired)")
	}
}

func TestRedisLocker_LockKey(t *testing.T) {
	_, client := setupMiniredis(t)

	tests := []struct {
		keyPrefix string
		jobName   string
		expected  string
	}{
		{
			keyPrefix: "cronlock:",
			jobName:   "my-job",
			expected:  "cronlock:job:my-job",
		},
		{
			keyPrefix: "",
			jobName:   "my-job",
			expected:  "job:my-job",
		},
		{
			keyPrefix: "prefix:",
			jobName:   "job-with-dashes",
			expected:  "prefix:job:job-with-dashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			locker := NewRedisLocker(client, "node", tt.keyPrefix)
			key := locker.lockKey(tt.jobName)
			if key != tt.expected {
				t.Errorf("lockKey(%q) = %q, want %q", tt.jobName, key, tt.expected)
			}
		})
	}
}

func TestRedisLocker_ReacquireAfterRelease(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	ctx := context.Background()

	// Acquire, release, then acquire again
	acquired1, _ := locker.Acquire(ctx, "test-job", 30*time.Second)
	if !acquired1 {
		t.Fatal("first Acquire() = false")
	}

	_ = locker.Release(ctx, "test-job")

	acquired2, err := locker.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if !acquired2 {
		t.Error("second Acquire() = false, want true (after release)")
	}
}

func TestRedisLocker_Acquire_AfterExpiry(t *testing.T) {
	s, client := setupMiniredis(t)

	locker1 := NewRedisLocker(client, "node-1", "test:")
	locker2 := NewRedisLocker(client, "node-2", "test:")

	ctx := context.Background()

	// First locker acquires with short TTL
	acquired1, _ := locker1.Acquire(ctx, "test-job", 5*time.Second)
	if !acquired1 {
		t.Fatal("first Acquire() = false")
	}

	// Fast-forward past TTL
	s.FastForward(10 * time.Second)

	// Second locker should be able to acquire
	acquired2, err := locker2.Acquire(ctx, "test-job", 30*time.Second)
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if !acquired2 {
		t.Error("second Acquire() = false, want true (after expiry)")
	}
}

func TestRedisLocker_Close(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")

	err := locker.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Client should be closed
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	if err == nil {
		t.Error("expected error after Close(), got nil")
	}
}

func TestRedisLocker_ConcurrentAccess(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")
	ctx := context.Background()

	const numJobs = 10
	const numIterations = 50

	var wg sync.WaitGroup
	wg.Add(numJobs)

	// Simulate multiple jobs running concurrently, each doing acquire/extend/release cycles
	for i := 0; i < numJobs; i++ {
		go func(jobNum int) {
			defer wg.Done()
			jobName := fmt.Sprintf("concurrent-job-%d", jobNum)

			for j := 0; j < numIterations; j++ {
				acquired, err := locker.Acquire(ctx, jobName, 30*time.Second)
				if err != nil {
					t.Errorf("Acquire() error = %v", err)
					return
				}

				if acquired {
					// Simulate some work and lock extension
					_, err := locker.Extend(ctx, jobName, 30*time.Second)
					if err != nil {
						t.Errorf("Extend() error = %v", err)
					}

					err = locker.Release(ctx, jobName)
					if err != nil {
						t.Errorf("Release() error = %v", err)
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestRedisLocker_ConcurrentDifferentOperations(t *testing.T) {
	_, client := setupMiniredis(t)

	locker := NewRedisLocker(client, "node-1", "test:")
	ctx := context.Background()

	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // 3 types of operations

	// Concurrent acquires on different jobs
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			jobName := fmt.Sprintf("job-%d", n)
			_, _ = locker.Acquire(ctx, jobName, 30*time.Second)
		}(i)
	}

	// Concurrent extends (some will fail as locks aren't held, which is fine)
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			jobName := fmt.Sprintf("job-%d", n)
			_, _ = locker.Extend(ctx, jobName, 30*time.Second)
		}(i)
	}

	// Concurrent releases (some will be no-ops, which is fine)
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			jobName := fmt.Sprintf("job-%d", n)
			_ = locker.Release(ctx, jobName)
		}(i)
	}

	wg.Wait()
}
