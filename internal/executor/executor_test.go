package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	// Save original SHELL
	originalShell := os.Getenv("SHELL")
	defer os.Setenv("SHELL", originalShell)

	tests := []struct {
		name          string
		shellEnv      string
		expectedShell string
	}{
		{
			name:          "uses SHELL env",
			shellEnv:      "/bin/bash",
			expectedShell: "/bin/bash",
		},
		{
			name:          "uses zsh",
			shellEnv:      "/bin/zsh",
			expectedShell: "/bin/zsh",
		},
		{
			name:          "defaults to /bin/sh when empty",
			shellEnv:      "",
			expectedShell: "/bin/sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("SHELL", tt.shellEnv)
			exec := New()
			if exec.shell != tt.expectedShell {
				t.Errorf("New().shell = %q, want %q", exec.shell, tt.expectedShell)
			}
		})
	}
}

func TestResult_Success(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		expected bool
	}{
		{
			name: "success with exit code 0 and no error",
			result: Result{
				ExitCode: 0,
				Err:      nil,
			},
			expected: true,
		},
		{
			name: "failure with non-zero exit code",
			result: Result{
				ExitCode: 1,
				Err:      nil,
			},
			expected: false,
		},
		{
			name: "failure with error",
			result: Result{
				ExitCode: 0,
				Err:      context.Canceled,
			},
			expected: false,
		},
		{
			name: "failure with both error and non-zero exit code",
			result: Result{
				ExitCode: 127,
				Err:      context.DeadlineExceeded,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Success(); got != tt.expected {
				t.Errorf("Success() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExecute_Success(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo hello",
	})

	if result.Err != nil {
		t.Errorf("Execute() Err = %v, want nil", result.Err)
	}
	if result.ExitCode != 0 {
		t.Errorf("Execute() ExitCode = %d, want 0", result.ExitCode)
	}
	if !result.Success() {
		t.Error("Execute() Success() = false, want true")
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "hello" {
		t.Errorf("Execute() Stdout = %q, want %q", stdout, "hello")
	}
	if result.Duration <= 0 {
		t.Errorf("Execute() Duration = %v, want > 0", result.Duration)
	}
}

func TestExecute_Stderr(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo error >&2",
	})

	if result.Err != nil {
		t.Errorf("Execute() Err = %v, want nil", result.Err)
	}

	stderr := strings.TrimSpace(result.Stderr)
	if stderr != "error" {
		t.Errorf("Execute() Stderr = %q, want %q", stderr, "error")
	}
}

func TestExecute_StdoutAndStderr(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo out && echo err >&2",
	})

	stdout := strings.TrimSpace(result.Stdout)
	stderr := strings.TrimSpace(result.Stderr)

	if stdout != "out" {
		t.Errorf("Stdout = %q, want %q", stdout, "out")
	}
	if stderr != "err" {
		t.Errorf("Stderr = %q, want %q", stderr, "err")
	}
}

func TestExecute_ExitCode(t *testing.T) {
	exec := New()
	ctx := context.Background()

	tests := []struct {
		name     string
		command  string
		exitCode int
	}{
		{
			name:     "exit 0",
			command:  "exit 0",
			exitCode: 0,
		},
		{
			name:     "exit 1",
			command:  "exit 1",
			exitCode: 1,
		},
		{
			name:     "exit 42",
			command:  "exit 42",
			exitCode: 42,
		},
		{
			name:     "exit 127",
			command:  "exit 127",
			exitCode: 127,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exec.Execute(ctx, Options{
				Command: tt.command,
			})

			if result.ExitCode != tt.exitCode {
				t.Errorf("Execute() ExitCode = %d, want %d", result.ExitCode, tt.exitCode)
			}

			if tt.exitCode == 0 {
				if result.Err != nil {
					t.Errorf("Execute() Err = %v, want nil for exit 0", result.Err)
				}
			} else {
				if result.Err == nil {
					t.Errorf("Execute() Err = nil, want error for exit %d", tt.exitCode)
				}
			}
		})
	}
}

func TestExecute_WorkDir(t *testing.T) {
	exec := New()
	ctx := context.Background()

	// Create a temporary directory
	tmpDir := t.TempDir()

	result := exec.Execute(ctx, Options{
		Command: "pwd",
		WorkDir: tmpDir,
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	// Resolve symlinks for comparison (macOS /tmp -> /private/tmp)
	expectedDir, _ := filepath.EvalSymlinks(tmpDir)
	actualDir, _ := filepath.EvalSymlinks(stdout)

	if actualDir != expectedDir {
		t.Errorf("pwd output = %q, want %q", actualDir, expectedDir)
	}
}

func TestExecute_Env(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo $TEST_VAR1-$TEST_VAR2",
		Env: map[string]string{
			"TEST_VAR1": "hello",
			"TEST_VAR2": "world",
		},
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "hello-world" {
		t.Errorf("Execute() Stdout = %q, want %q", stdout, "hello-world")
	}
}

func TestExecute_Env_InheritsParent(t *testing.T) {
	exec := New()
	ctx := context.Background()

	// Set an environment variable in parent process
	os.Setenv("CRONLOCK_TEST_INHERIT", "inherited")
	defer os.Unsetenv("CRONLOCK_TEST_INHERIT")

	result := exec.Execute(ctx, Options{
		Command: "echo $CRONLOCK_TEST_INHERIT",
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "inherited" {
		t.Errorf("Execute() Stdout = %q, want %q (inherited from parent)", stdout, "inherited")
	}
}

func TestExecute_Env_Overrides(t *testing.T) {
	exec := New()
	ctx := context.Background()

	// Set an environment variable in parent process
	os.Setenv("CRONLOCK_TEST_OVERRIDE", "original")
	defer os.Unsetenv("CRONLOCK_TEST_OVERRIDE")

	result := exec.Execute(ctx, Options{
		Command: "echo $CRONLOCK_TEST_OVERRIDE",
		Env: map[string]string{
			"CRONLOCK_TEST_OVERRIDE": "overridden",
		},
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "overridden" {
		t.Errorf("Execute() Stdout = %q, want %q (should override parent)", stdout, "overridden")
	}
}

func TestExecute_Timeout(t *testing.T) {
	exec := New()

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := exec.Execute(ctx, Options{
		Command: "sleep 10",
	})
	elapsed := time.Since(start)

	// Should have been cancelled
	if result.Err == nil {
		t.Error("Execute() Err = nil, want context deadline exceeded")
	}

	// Should not have taken 10 seconds
	if elapsed > 1*time.Second {
		t.Errorf("Execute() took %v, should have timed out much sooner", elapsed)
	}

	// Exit code should indicate failure
	if result.ExitCode == 0 {
		t.Error("Execute() ExitCode = 0, want non-zero for timeout")
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	exec := New()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := exec.Execute(ctx, Options{
		Command: "sleep 10",
	})
	elapsed := time.Since(start)

	// Should have been cancelled
	if result.Err == nil {
		t.Error("Execute() Err = nil, want context cancelled")
	}

	// Should not have taken 10 seconds
	if elapsed > 1*time.Second {
		t.Errorf("Execute() took %v, should have been cancelled sooner", elapsed)
	}
}

func TestExecute_MultilineOutput(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo line1; echo line2; echo line3",
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
	expected := []string{"line1", "line2", "line3"}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("line[%d] = %q, want %q", i, line, expected[i])
		}
	}
}

func TestExecute_CommandNotFound(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "nonexistent_command_12345",
	})

	if result.Err == nil {
		t.Error("Execute() Err = nil, want error for command not found")
	}
	if result.ExitCode == 0 {
		t.Error("Execute() ExitCode = 0, want non-zero for command not found")
	}
}

func TestExecute_EmptyCommand(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "",
	})

	// Empty command should succeed with exit code 0
	if result.ExitCode != 0 {
		t.Errorf("Execute() ExitCode = %d, want 0 for empty command", result.ExitCode)
	}
}

func TestExecute_ShellPipes(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo hello world | tr ' ' '-'",
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "hello-world" {
		t.Errorf("Execute() Stdout = %q, want %q", stdout, "hello-world")
	}
}

func TestExecute_ShellExpansion(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "echo $((2 + 2))",
	})

	if result.Err != nil {
		t.Fatalf("Execute() Err = %v", result.Err)
	}

	stdout := strings.TrimSpace(result.Stdout)
	if stdout != "4" {
		t.Errorf("Execute() Stdout = %q, want %q", stdout, "4")
	}
}

func TestExecute_Duration(t *testing.T) {
	exec := New()
	ctx := context.Background()

	result := exec.Execute(ctx, Options{
		Command: "sleep 0.1",
	})

	if result.Duration < 100*time.Millisecond {
		t.Errorf("Execute() Duration = %v, want >= 100ms", result.Duration)
	}
}
