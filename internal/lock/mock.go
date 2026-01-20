package lock

import (
	"context"
	"sync"
	"time"
)

// MockLocker is a test implementation of the Locker interface.
type MockLocker struct {
	mu sync.Mutex

	// Configurable return values
	AcquireResult bool
	AcquireError  error
	ReleaseError  error
	ExtendResult  bool
	ExtendError   error
	CloseError    error

	// Call tracking
	AcquireCalls []AcquireCall
	ReleaseCalls []string
	ExtendCalls  []ExtendCall

	// Simulate held locks
	heldLocks map[string]bool
}

// AcquireCall records an Acquire call.
type AcquireCall struct {
	JobName string
	TTL     time.Duration
}

// ExtendCall records an Extend call.
type ExtendCall struct {
	JobName string
	TTL     time.Duration
}

// NewMockLocker creates a new MockLocker with default success behavior.
func NewMockLocker() *MockLocker {
	return &MockLocker{
		AcquireResult: true,
		ExtendResult:  true,
		heldLocks:     make(map[string]bool),
	}
}

// Acquire implements Locker.Acquire.
func (m *MockLocker) Acquire(ctx context.Context, jobName string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AcquireCalls = append(m.AcquireCalls, AcquireCall{JobName: jobName, TTL: ttl})

	if m.AcquireError != nil {
		return false, m.AcquireError
	}

	// Simulate actual lock behavior if configured
	if m.heldLocks[jobName] {
		return false, nil
	}

	if m.AcquireResult {
		m.heldLocks[jobName] = true
	}

	return m.AcquireResult, nil
}

// Release implements Locker.Release.
func (m *MockLocker) Release(ctx context.Context, jobName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ReleaseCalls = append(m.ReleaseCalls, jobName)

	if m.ReleaseError != nil {
		return m.ReleaseError
	}

	delete(m.heldLocks, jobName)
	return nil
}

// Extend implements Locker.Extend.
func (m *MockLocker) Extend(ctx context.Context, jobName string, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ExtendCalls = append(m.ExtendCalls, ExtendCall{JobName: jobName, TTL: ttl})

	if m.ExtendError != nil {
		return false, m.ExtendError
	}

	return m.ExtendResult, nil
}

// Close implements Locker.Close.
func (m *MockLocker) Close() error {
	return m.CloseError
}

// SetLockHeld simulates a lock being held (for testing contention).
func (m *MockLocker) SetLockHeld(jobName string, held bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if held {
		m.heldLocks[jobName] = true
	} else {
		delete(m.heldLocks, jobName)
	}
}

// Reset clears all call tracking and held locks.
func (m *MockLocker) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AcquireCalls = nil
	m.ReleaseCalls = nil
	m.ExtendCalls = nil
	m.heldLocks = make(map[string]bool)
}
