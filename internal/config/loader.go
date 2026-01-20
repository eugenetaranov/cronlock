package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Load reads and parses a configuration file. Supports YAML and TOML formats
// based on file extension. Environment variables in the format ${VAR} or
// ${VAR:-default} are substituted.
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	ext := strings.ToLower(filepath.Ext(path))
	var parser koanf.Parser

	switch ext {
	case ".yaml", ".yml":
		parser = yaml.Parser()
	case ".toml":
		parser = toml.Parser()
	default:
		return nil, fmt.Errorf("unsupported config format: %s", ext)
	}

	if err := k.Load(file.Provider(path), parser); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	cfg := Defaults()
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Expand environment variables in string fields
	expandEnvInConfig(&cfg)

	// Validate configuration
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// expandEnvInConfig expands environment variables in configuration values.
func expandEnvInConfig(cfg *Config) {
	cfg.Node.ID = expandEnv(cfg.Node.ID)
	cfg.Redis.Address = expandEnv(cfg.Redis.Address)
	cfg.Redis.Password = expandEnv(cfg.Redis.Password)
	cfg.Redis.KeyPrefix = expandEnv(cfg.Redis.KeyPrefix)

	for i := range cfg.Jobs {
		cfg.Jobs[i].Name = expandEnv(cfg.Jobs[i].Name)
		cfg.Jobs[i].Command = expandEnv(cfg.Jobs[i].Command)
		cfg.Jobs[i].WorkDir = expandEnv(cfg.Jobs[i].WorkDir)
		cfg.Jobs[i].OnFailure = expandEnv(cfg.Jobs[i].OnFailure)
		cfg.Jobs[i].OnSuccess = expandEnv(cfg.Jobs[i].OnSuccess)
		for k, v := range cfg.Jobs[i].Env {
			cfg.Jobs[i].Env[k] = expandEnv(v)
		}
	}
}

// expandEnv expands environment variables in a string.
// Supports ${VAR} and ${VAR:-default} syntax.
func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		// Handle default value syntax: VAR:-default
		if idx := strings.Index(key, ":-"); idx != -1 {
			varName := key[:idx]
			defaultVal := key[idx+2:]
			if val := os.Getenv(varName); val != "" {
				return val
			}
			return defaultVal
		}
		return os.Getenv(key)
	})
}

// validate checks the configuration for errors.
func validate(cfg *Config) error {
	if cfg.Redis.Address == "" {
		return fmt.Errorf("redis.address is required")
	}

	seen := make(map[string]int)
	for i, job := range cfg.Jobs {
		if job.Name == "" {
			return fmt.Errorf("jobs[%d].name is required", i)
		}
		if prev, exists := seen[job.Name]; exists {
			return fmt.Errorf("jobs[%d].name %q is a duplicate of jobs[%d]", i, job.Name, prev)
		}
		seen[job.Name] = i
		if job.Schedule == "" {
			return fmt.Errorf("jobs[%d].schedule is required", i)
		}
		if job.Command == "" {
			return fmt.Errorf("jobs[%d].command is required", i)
		}
	}

	return nil
}
