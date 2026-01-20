package lock

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Lua script for atomic release: only delete if value matches.
var releaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
else
	return 0
end
`)

// Lua script for atomic extend: only extend TTL if value matches.
var extendScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
else
	return 0
end
`)

// RedisLocker implements distributed locking using Redis.
type RedisLocker struct {
	client    *redis.Client
	nodeID    string
	keyPrefix string
	locks     map[string]string // jobName -> lockValue
}

// NewRedisLocker creates a new Redis-based locker.
func NewRedisLocker(client *redis.Client, nodeID, keyPrefix string) *RedisLocker {
	return &RedisLocker{
		client:    client,
		nodeID:    nodeID,
		keyPrefix: keyPrefix,
		locks:     make(map[string]string),
	}
}

// lockKey returns the Redis key for a job lock.
func (r *RedisLocker) lockKey(jobName string) string {
	return fmt.Sprintf("%sjob:%s", r.keyPrefix, jobName)
}

// lockValue generates a unique value for this lock acquisition.
func (r *RedisLocker) lockValue() string {
	return fmt.Sprintf("%s:%s", r.nodeID, uuid.New().String())
}

// Acquire attempts to acquire a lock using SET NX EX.
func (r *RedisLocker) Acquire(ctx context.Context, jobName string, ttl time.Duration) (bool, error) {
	key := r.lockKey(jobName)
	value := r.lockValue()

	// SET key value NX PX milliseconds
	ok, err := r.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if ok {
		r.locks[jobName] = value
	}

	return ok, nil
}

// Release releases the lock using a Lua script for atomicity.
func (r *RedisLocker) Release(ctx context.Context, jobName string) error {
	key := r.lockKey(jobName)
	value, ok := r.locks[jobName]
	if !ok {
		// We don't own this lock
		return nil
	}

	result, err := releaseScript.Run(ctx, r.client, []string{key}, value).Int64()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	delete(r.locks, jobName)

	if result == 0 {
		// Lock was already released or owned by someone else
		return nil
	}

	return nil
}

// Extend extends the lock TTL using a Lua script for atomicity.
func (r *RedisLocker) Extend(ctx context.Context, jobName string, ttl time.Duration) (bool, error) {
	key := r.lockKey(jobName)
	value, ok := r.locks[jobName]
	if !ok {
		// We don't own this lock
		return false, nil
	}

	result, err := extendScript.Run(ctx, r.client, []string{key}, value, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("failed to extend lock: %w", err)
	}

	return result == 1, nil
}

// Close releases any resources held by the locker.
func (r *RedisLocker) Close() error {
	return r.client.Close()
}
