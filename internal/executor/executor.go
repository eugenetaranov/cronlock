package executor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Result represents the result of a command execution.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Duration time.Duration
	Err      error
}

// Success returns true if the command executed successfully (exit code 0).
func (r *Result) Success() bool {
	return r.Err == nil && r.ExitCode == 0
}

// Executor handles shell command execution.
type Executor struct {
	shell string
}

// New creates a new Executor.
func New() *Executor {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return &Executor{shell: shell}
}

// Execute runs a command with the given options.
func (e *Executor) Execute(ctx context.Context, opts Options) *Result {
	start := time.Now()
	result := &Result{}

	// Create command with shell
	cmd := exec.CommandContext(ctx, e.shell, "-c", opts.Command)

	// Set working directory if specified
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}

	// Set environment variables
	cmd.Env = os.Environ()
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture stdout and stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	if err != nil {
		result.Err = err
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// Options contains execution options for a command.
type Options struct {
	Command string
	WorkDir string
	Env     map[string]string
	Timeout time.Duration
}
