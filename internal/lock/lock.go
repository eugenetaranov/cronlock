package lock

import (
	"context"
	"time"
)

// Locker defines the interface for distributed locking.
type Locker interface {
	// Acquire attempts to acquire a lock for the given job name.
	// Returns true if the lock was acquired, false otherwise.
	Acquire(ctx context.Context, jobName string, ttl time.Duration) (bool, error)

	// Release releases the lock for the given job name.
	// Only releases if the current node owns the lock.
	Release(ctx context.Context, jobName string) error

	// Extend extends the TTL of an existing lock.
	// Only extends if the current node owns the lock.
	Extend(ctx context.Context, jobName string, ttl time.Duration) (bool, error)

	// Close releases any resources held by the locker.
	Close() error
}

// Lock represents an acquired distributed lock.
type Lock struct {
	JobName string
	Value   string
	TTL     time.Duration
}
