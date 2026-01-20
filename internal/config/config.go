package config

import "time"

// Config represents the complete application configuration.
type Config struct {
	Node  NodeConfig  `koanf:"node"`
	Redis RedisConfig `koanf:"redis"`
	Jobs  []JobConfig `koanf:"jobs"`
}

// NodeConfig contains node-specific settings.
type NodeConfig struct {
	ID          string        `koanf:"id"`
	GracePeriod time.Duration `koanf:"grace_period"`
}

// RedisConfig contains Redis connection settings.
type RedisConfig struct {
	Address   string `koanf:"address"`
	Password  string `koanf:"password"`
	DB        int    `koanf:"db"`
	KeyPrefix string `koanf:"key_prefix"`
}

// JobConfig defines a scheduled job.
type JobConfig struct {
	Name       string            `koanf:"name"`
	Schedule   string            `koanf:"schedule"`
	Command    string            `koanf:"command"`
	Timeout    time.Duration     `koanf:"timeout"`
	LockTTL    time.Duration     `koanf:"lock_ttl"`
	WorkDir    string            `koanf:"work_dir"`
	Env        map[string]string `koanf:"env"`
	OnFailure  string            `koanf:"on_failure"`
	OnSuccess  string            `koanf:"on_success"`
	Enabled    *bool             `koanf:"enabled"`
}

// IsEnabled returns whether the job is enabled. Defaults to true if not specified.
func (j JobConfig) IsEnabled() bool {
	if j.Enabled == nil {
		return true
	}
	return *j.Enabled
}

// Defaults returns a Config with sensible default values.
func Defaults() Config {
	return Config{
		Node: NodeConfig{
			ID:          "",
			GracePeriod: 5 * time.Second,
		},
		Redis: RedisConfig{
			Address:   "localhost:6379",
			KeyPrefix: "cronlock:",
		},
		Jobs: []JobConfig{},
	}
}
