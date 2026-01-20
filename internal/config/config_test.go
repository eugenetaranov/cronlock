package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.Node.ID != "" {
		t.Errorf("expected empty Node.ID, got %q", cfg.Node.ID)
	}
	if cfg.Node.GracePeriod != 5*time.Second {
		t.Errorf("expected GracePeriod 5s, got %v", cfg.Node.GracePeriod)
	}
	if cfg.Redis.Address != "localhost:6379" {
		t.Errorf("expected Redis.Address localhost:6379, got %q", cfg.Redis.Address)
	}
	if cfg.Redis.KeyPrefix != "cronlock:" {
		t.Errorf("expected Redis.KeyPrefix cronlock:, got %q", cfg.Redis.KeyPrefix)
	}
	if cfg.Redis.DB != 0 {
		t.Errorf("expected Redis.DB 0, got %d", cfg.Redis.DB)
	}
	if cfg.Redis.Password != "" {
		t.Errorf("expected empty Redis.Password, got %q", cfg.Redis.Password)
	}
	if len(cfg.Jobs) != 0 {
		t.Errorf("expected empty Jobs slice, got %d jobs", len(cfg.Jobs))
	}
}

func TestJobConfig_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  *bool
		expected bool
	}{
		{
			name:     "nil defaults to true",
			enabled:  nil,
			expected: true,
		},
		{
			name:     "explicit true",
			enabled:  boolPtr(true),
			expected: true,
		},
		{
			name:     "explicit false",
			enabled:  boolPtr(false),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := JobConfig{Enabled: tt.enabled}
			if got := job.IsEnabled(); got != tt.expected {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

func TestLoad_YAML(t *testing.T) {
	content := `
node:
  id: test-node
  grace_period: 10s

redis:
  address: localhost:6380
  password: secret
  db: 1
  key_prefix: "test:"

jobs:
  - name: test-job
    schedule: "* * * * *"
    command: echo hello
    timeout: 30s
    lock_ttl: 60s
    work_dir: /tmp
    env:
      FOO: "bar"
    on_success: echo success
    on_failure: echo failure
`
	tmpFile := writeTempFile(t, "config.yaml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Node.ID != "test-node" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "test-node")
	}
	if cfg.Node.GracePeriod != 10*time.Second {
		t.Errorf("Node.GracePeriod = %v, want %v", cfg.Node.GracePeriod, 10*time.Second)
	}
	if cfg.Redis.Address != "localhost:6380" {
		t.Errorf("Redis.Address = %q, want %q", cfg.Redis.Address, "localhost:6380")
	}
	if cfg.Redis.Password != "secret" {
		t.Errorf("Redis.Password = %q, want %q", cfg.Redis.Password, "secret")
	}
	if cfg.Redis.DB != 1 {
		t.Errorf("Redis.DB = %d, want %d", cfg.Redis.DB, 1)
	}
	if cfg.Redis.KeyPrefix != "test:" {
		t.Errorf("Redis.KeyPrefix = %q, want %q", cfg.Redis.KeyPrefix, "test:")
	}

	if len(cfg.Jobs) != 1 {
		t.Fatalf("len(Jobs) = %d, want 1", len(cfg.Jobs))
	}

	job := cfg.Jobs[0]
	if job.Name != "test-job" {
		t.Errorf("Job.Name = %q, want %q", job.Name, "test-job")
	}
	if job.Schedule != "* * * * *" {
		t.Errorf("Job.Schedule = %q, want %q", job.Schedule, "* * * * *")
	}
	if job.Command != "echo hello" {
		t.Errorf("Job.Command = %q, want %q", job.Command, "echo hello")
	}
	if job.Timeout != 30*time.Second {
		t.Errorf("Job.Timeout = %v, want %v", job.Timeout, 30*time.Second)
	}
	if job.LockTTL != 60*time.Second {
		t.Errorf("Job.LockTTL = %v, want %v", job.LockTTL, 60*time.Second)
	}
	if job.WorkDir != "/tmp" {
		t.Errorf("Job.WorkDir = %q, want %q", job.WorkDir, "/tmp")
	}
	if job.Env["FOO"] != "bar" {
		t.Errorf("Job.Env[FOO] = %q, want %q", job.Env["FOO"], "bar")
	}
	if job.OnSuccess != "echo success" {
		t.Errorf("Job.OnSuccess = %q, want %q", job.OnSuccess, "echo success")
	}
	if job.OnFailure != "echo failure" {
		t.Errorf("Job.OnFailure = %q, want %q", job.OnFailure, "echo failure")
	}
}

func TestLoad_YML_Extension(t *testing.T) {
	content := `
redis:
  address: localhost:6379
jobs:
  - name: test
    schedule: "* * * * *"
    command: echo test
`
	tmpFile := writeTempFile(t, "config.yml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(cfg.Jobs))
	}
}

func TestLoad_TOML(t *testing.T) {
	content := `
[node]
id = "toml-node"
grace_period = "15s"

[redis]
address = "localhost:6381"
password = "toml-secret"
db = 2
key_prefix = "toml:"

[[jobs]]
name = "toml-job"
schedule = "*/5 * * * *"
command = "echo toml"
timeout = "45s"
lock_ttl = "90s"
`
	tmpFile := writeTempFile(t, "config.toml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Node.ID != "toml-node" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "toml-node")
	}
	if cfg.Node.GracePeriod != 15*time.Second {
		t.Errorf("Node.GracePeriod = %v, want %v", cfg.Node.GracePeriod, 15*time.Second)
	}
	if cfg.Redis.Address != "localhost:6381" {
		t.Errorf("Redis.Address = %q, want %q", cfg.Redis.Address, "localhost:6381")
	}
	if cfg.Redis.Password != "toml-secret" {
		t.Errorf("Redis.Password = %q, want %q", cfg.Redis.Password, "toml-secret")
	}
	if cfg.Redis.DB != 2 {
		t.Errorf("Redis.DB = %d, want %d", cfg.Redis.DB, 2)
	}
	if cfg.Redis.KeyPrefix != "toml:" {
		t.Errorf("Redis.KeyPrefix = %q, want %q", cfg.Redis.KeyPrefix, "toml:")
	}

	if len(cfg.Jobs) != 1 {
		t.Fatalf("len(Jobs) = %d, want 1", len(cfg.Jobs))
	}

	job := cfg.Jobs[0]
	if job.Name != "toml-job" {
		t.Errorf("Job.Name = %q, want %q", job.Name, "toml-job")
	}
	if job.Schedule != "*/5 * * * *" {
		t.Errorf("Job.Schedule = %q, want %q", job.Schedule, "*/5 * * * *")
	}
	if job.Command != "echo toml" {
		t.Errorf("Job.Command = %q, want %q", job.Command, "echo toml")
	}
	if job.Timeout != 45*time.Second {
		t.Errorf("Job.Timeout = %v, want %v", job.Timeout, 45*time.Second)
	}
	if job.LockTTL != 90*time.Second {
		t.Errorf("Job.LockTTL = %v, want %v", job.LockTTL, 90*time.Second)
	}
}

func TestLoad_UnsupportedFormat(t *testing.T) {
	tmpFile := writeTempFile(t, "config.json", `{"test": true}`)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected error for unsupported format, got nil")
	}

	expected := "unsupported config format: .json"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_EnvSubstitution(t *testing.T) {
	// Set test environment variables
	os.Setenv("CRONLOCK_TEST_NODE_ID", "env-node")
	os.Setenv("CRONLOCK_TEST_REDIS_ADDR", "redis.example.com:6379")
	defer func() {
		os.Unsetenv("CRONLOCK_TEST_NODE_ID")
		os.Unsetenv("CRONLOCK_TEST_REDIS_ADDR")
	}()

	content := `
node:
  id: ${CRONLOCK_TEST_NODE_ID}

redis:
  address: ${CRONLOCK_TEST_REDIS_ADDR}
  password: ${CRONLOCK_TEST_MISSING:-default-password}
  key_prefix: ${CRONLOCK_TEST_PREFIX:-cronlock:}

jobs:
  - name: env-job
    schedule: "* * * * *"
    command: echo ${CRONLOCK_TEST_MESSAGE:-hello}
`
	tmpFile := writeTempFile(t, "config-env.yaml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Node.ID != "env-node" {
		t.Errorf("Node.ID = %q, want %q", cfg.Node.ID, "env-node")
	}
	if cfg.Redis.Address != "redis.example.com:6379" {
		t.Errorf("Redis.Address = %q, want %q", cfg.Redis.Address, "redis.example.com:6379")
	}
	if cfg.Redis.Password != "default-password" {
		t.Errorf("Redis.Password = %q, want %q (default)", cfg.Redis.Password, "default-password")
	}
	if cfg.Redis.KeyPrefix != "cronlock:" {
		t.Errorf("Redis.KeyPrefix = %q, want %q (default)", cfg.Redis.KeyPrefix, "cronlock:")
	}
	if cfg.Jobs[0].Command != "echo hello" {
		t.Errorf("Job.Command = %q, want %q", cfg.Jobs[0].Command, "echo hello")
	}
}

func TestLoad_EnvSubstitution_Precedence(t *testing.T) {
	// Set environment variable
	os.Setenv("CRONLOCK_TEST_KEY_PREFIX", "custom-prefix:")
	defer os.Unsetenv("CRONLOCK_TEST_KEY_PREFIX")

	content := `
redis:
  address: localhost:6379
  key_prefix: ${CRONLOCK_TEST_KEY_PREFIX:-default:}

jobs:
  - name: test
    schedule: "* * * * *"
    command: echo test
`
	tmpFile := writeTempFile(t, "config-env-precedence.yaml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Environment variable should take precedence over default
	if cfg.Redis.KeyPrefix != "custom-prefix:" {
		t.Errorf("Redis.KeyPrefix = %q, want %q", cfg.Redis.KeyPrefix, "custom-prefix:")
	}
}

func TestLoad_Validation_MissingRedisAddress(t *testing.T) {
	content := `
redis:
  address: ""

jobs:
  - name: test
    schedule: "* * * * *"
    command: echo test
`
	tmpFile := writeTempFile(t, "config-invalid.yaml", content)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected validation error, got nil")
	}

	expected := "redis.address is required"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_Validation_MissingJobName(t *testing.T) {
	content := `
redis:
  address: localhost:6379

jobs:
  - name: ""
    schedule: "* * * * *"
    command: echo test
`
	tmpFile := writeTempFile(t, "config-invalid-job-name.yaml", content)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected validation error, got nil")
	}

	expected := "jobs[0].name is required"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_Validation_MissingJobSchedule(t *testing.T) {
	content := `
redis:
  address: localhost:6379

jobs:
  - name: test
    schedule: ""
    command: echo test
`
	tmpFile := writeTempFile(t, "config-invalid-job-schedule.yaml", content)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected validation error, got nil")
	}

	expected := "jobs[0].schedule is required"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_Validation_MissingJobCommand(t *testing.T) {
	content := `
redis:
  address: localhost:6379

jobs:
  - name: test
    schedule: "* * * * *"
    command: ""
`
	tmpFile := writeTempFile(t, "config-invalid-job-command.yaml", content)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected validation error, got nil")
	}

	expected := "jobs[0].command is required"
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_Validation_DuplicateJobName(t *testing.T) {
	content := `
redis:
  address: localhost:6379

jobs:
  - name: my-job
    schedule: "* * * * *"
    command: echo first
  - name: my-job
    schedule: "*/5 * * * *"
    command: echo second
`
	tmpFile := writeTempFile(t, "config-duplicate-job-name.yaml", content)
	defer os.Remove(tmpFile)

	_, err := Load(tmpFile)
	if err == nil {
		t.Error("expected validation error, got nil")
	}

	expected := `jobs[1].name "my-job" is a duplicate of jobs[0]`
	if err.Error() != expected {
		t.Errorf("error = %q, want %q", err.Error(), expected)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestLoad_MultipleJobs(t *testing.T) {
	content := `
redis:
  address: localhost:6379

jobs:
  - name: job1
    schedule: "* * * * *"
    command: echo job1
  - name: job2
    schedule: "*/5 * * * *"
    command: echo job2
  - name: job3
    schedule: "0 * * * *"
    command: echo job3
`
	tmpFile := writeTempFile(t, "config-multi.yaml", content)
	defer os.Remove(tmpFile)

	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Jobs) != 3 {
		t.Errorf("len(Jobs) = %d, want 3", len(cfg.Jobs))
	}

	names := []string{cfg.Jobs[0].Name, cfg.Jobs[1].Name, cfg.Jobs[2].Name}
	expected := []string{"job1", "job2", "job3"}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("Jobs[%d].Name = %q, want %q", i, name, expected[i])
		}
	}
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	return path
}
