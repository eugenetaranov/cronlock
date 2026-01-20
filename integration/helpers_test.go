//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

var (
	buildOnce   sync.Once
	binaryPath  string
	buildErr    error
	projectRoot string
)

// JobDef defines a job for test configuration.
type JobDef struct {
	Name      string
	Schedule  string
	Command   string
	Timeout   string
	LockTTL   string
	WorkDir   string
	OnSuccess string
	OnFailure string
}

// RedisContainer wraps a testcontainers Redis instance.
type RedisContainer struct {
	container testcontainers.Container
	addr      string
	client    *redis.Client
}

// setupRedis starts a Redis container and returns connection info.
func setupRedis(ctx context.Context) (*RedisContainer, error) {
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		return nil, fmt.Errorf("failed to start redis container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get redis host: %w", err)
	}

	port, err := container.MappedPort(ctx, "6379")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get redis port: %w", err)
	}

	addr := fmt.Sprintf("%s:%s", host, port.Port())

	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	// Verify connection
	if err := client.Ping(ctx).Err(); err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &RedisContainer{
		container: container,
		addr:      addr,
		client:    client,
	}, nil
}

// Addr returns the Redis address.
func (r *RedisContainer) Addr() string {
	return r.addr
}

// Client returns the Redis client.
func (r *RedisContainer) Client() *redis.Client {
	return r.client
}

// Terminate stops the Redis container.
func (r *RedisContainer) Terminate(ctx context.Context) error {
	if r.client != nil {
		r.client.Close()
	}
	if r.container != nil {
		return r.container.Terminate(ctx)
	}
	return nil
}

// LockExists checks if a lock key exists in Redis.
func (r *RedisContainer) LockExists(ctx context.Context, jobName string) (bool, error) {
	key := fmt.Sprintf("cronlock:job:%s", jobName)
	result, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// GetLockTTL returns the TTL of a lock key.
func (r *RedisContainer) GetLockTTL(ctx context.Context, jobName string) (time.Duration, error) {
	key := fmt.Sprintf("cronlock:job:%s", jobName)
	return r.client.TTL(ctx, key).Result()
}

// buildCronlock builds the cronlock binary once and returns the path.
func buildCronlock(t *testing.T) string {
	buildOnce.Do(func() {
		// Find project root
		wd, err := os.Getwd()
		if err != nil {
			buildErr = fmt.Errorf("failed to get working directory: %w", err)
			return
		}

		// Navigate to project root (parent of integration/)
		projectRoot = filepath.Dir(wd)

		// Build binary
		binaryPath = filepath.Join(projectRoot, "cronlock-test")
		cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/cronlock")
		cmd.Dir = projectRoot
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			buildErr = fmt.Errorf("failed to build cronlock: %w", err)
			return
		}
	})

	if buildErr != nil {
		t.Fatalf("build failed: %v", buildErr)
	}

	return binaryPath
}

// writeTestConfig generates a YAML config file and returns the path.
func writeTestConfig(t *testing.T, redisAddr string, jobs []JobDef) string {
	t.Helper()

	configContent := fmt.Sprintf(`node:
  id: "test-node-%d"
  grace_period: 1s

redis:
  address: "%s"
  key_prefix: "cronlock:"

jobs:
`, time.Now().UnixNano(), redisAddr)

	for _, job := range jobs {
		configContent += fmt.Sprintf(`  - name: "%s"
    schedule: "%s"
    command: "%s"
`, job.Name, job.Schedule, job.Command)

		if job.Timeout != "" {
			configContent += fmt.Sprintf(`    timeout: %s
`, job.Timeout)
		}
		if job.LockTTL != "" {
			configContent += fmt.Sprintf(`    lock_ttl: %s
`, job.LockTTL)
		}
		if job.WorkDir != "" {
			configContent += fmt.Sprintf(`    work_dir: "%s"
`, job.WorkDir)
		}
		if job.OnSuccess != "" {
			configContent += fmt.Sprintf(`    on_success: "%s"
`, job.OnSuccess)
		}
		if job.OnFailure != "" {
			configContent += fmt.Sprintf(`    on_failure: "%s"
`, job.OnFailure)
		}
	}

	configPath := filepath.Join(t.TempDir(), "cronlock.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return configPath
}

// writeTestConfigWithNodeID generates a YAML config with a specific node ID.
func writeTestConfigWithNodeID(t *testing.T, redisAddr, nodeID string, jobs []JobDef) string {
	t.Helper()

	configContent := fmt.Sprintf(`node:
  id: "%s"
  grace_period: 1s

redis:
  address: "%s"
  key_prefix: "cronlock:"

jobs:
`, nodeID, redisAddr)

	for _, job := range jobs {
		configContent += fmt.Sprintf(`  - name: "%s"
    schedule: "%s"
    command: "%s"
`, job.Name, job.Schedule, job.Command)

		if job.Timeout != "" {
			configContent += fmt.Sprintf(`    timeout: %s
`, job.Timeout)
		}
		if job.LockTTL != "" {
			configContent += fmt.Sprintf(`    lock_ttl: %s
`, job.LockTTL)
		}
		if job.WorkDir != "" {
			configContent += fmt.Sprintf(`    work_dir: "%s"
`, job.WorkDir)
		}
	}

	configPath := filepath.Join(t.TempDir(), fmt.Sprintf("cronlock-%s.yaml", nodeID))
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	return configPath
}

// CronlockProcess wraps a cronlock process.
type CronlockProcess struct {
	cmd    *exec.Cmd
	stdout *os.File
	stderr *os.File
}

// startCronlock starts a cronlock instance with the given config.
func startCronlock(t *testing.T, ctx context.Context, configPath string) *CronlockProcess {
	t.Helper()

	binaryPath := buildCronlock(t)

	// Create temp files for stdout/stderr
	stdout, err := os.CreateTemp("", "cronlock-stdout-*")
	if err != nil {
		t.Fatalf("failed to create stdout file: %v", err)
	}

	stderr, err := os.CreateTemp("", "cronlock-stderr-*")
	if err != nil {
		stdout.Close()
		t.Fatalf("failed to create stderr file: %v", err)
	}

	cmd := exec.CommandContext(ctx, binaryPath, "-config", configPath)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		t.Fatalf("failed to start cronlock: %v", err)
	}

	// Wait for process to initialize and connect to Redis
	time.Sleep(500 * time.Millisecond)

	return &CronlockProcess{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
	}
}

// Stop stops the cronlock process gracefully.
func (p *CronlockProcess) Stop() error {
	if p.cmd.Process == nil {
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		// Process may have already exited
		return nil
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		p.cmd.Process.Kill()
		return fmt.Errorf("process did not exit gracefully")
	}
}

// Kill forcefully kills the cronlock process.
func (p *CronlockProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Cleanup closes files and removes temp files.
func (p *CronlockProcess) Cleanup() {
	if p.stdout != nil {
		name := p.stdout.Name()
		p.stdout.Close()
		os.Remove(name)
	}
	if p.stderr != nil {
		name := p.stderr.Name()
		p.stderr.Close()
		os.Remove(name)
	}
}

// Logs returns the stderr output.
func (p *CronlockProcess) Logs() string {
	if p.stderr == nil {
		return ""
	}
	p.stderr.Seek(0, 0)
	data, _ := os.ReadFile(p.stderr.Name())
	return string(data)
}

// waitForFile waits for a file to exist and optionally have minimum content.
func waitForFile(path string, timeout time.Duration, minSize int64) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil && info.Size() >= minSize {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for file %s", path)
}

// countLines counts non-empty lines in a file.
func countLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}

	// Count last line if no trailing newline
	if len(data) > 0 && data[len(data)-1] != '\n' {
		count++
	}

	return count, nil
}

// countOccurrences counts occurrences of a substring in a file.
func countOccurrences(path, substr string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	content := string(data)
	for i := 0; i < len(content); {
		idx := indexOf(content[i:], substr)
		if idx == -1 {
			break
		}
		count++
		i += idx + len(substr)
	}

	return count, nil
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
