# Cronlock

A distributed cron scheduler with Redis-based locking for exactly-once job execution across multiple nodes.

## Features

- **Distributed locking**: Uses Redis `SET NX EX` for atomic lock acquisition
- **Exactly-once execution**: Only one node executes each scheduled job
- **Lock renewal**: Automatically extends locks for long-running jobs
- **Graceful failover**: If a node dies, another takes over on the next schedule
- **Flexible scheduling**: Standard cron expressions with optional seconds field
- **Systemd integration**: Notify and watchdog support
- **Environment variables**: Supports `${VAR}` and `${VAR:-default}` syntax in config

## Why Cronlock over Regular Cron?

| Feature | Cron | Cronlock |
|---------|------|----------|
| Multi-node execution | Runs on every node | Exactly-once via Redis locking |
| Failure handling | Silent failures, check logs manually | `on_failure` hooks for immediate alerts |
| Success confirmation | No built-in notification | `on_success` hooks for confirmation |
| Long-running jobs | Can overlap if job exceeds interval | Lock renewal prevents overlap |
| Node failure | Jobs stop running | Automatic failover to healthy nodes |
| Monitoring | Parse syslogs or mail | Hooks integrate with any alerting system |

**Key advantages:**

1. **Distributed coordination** - The core differentiator; cron has no awareness of other nodes running the same job
2. **Built-in observability** - `on_success`/`on_failure` hooks make it trivial to integrate with Slack, PagerDuty, or any alerting system
3. **No overlapping runs** - Lock-based execution prevents the classic "job still running when next schedule fires" problem

## Installation

### Linux (Recommended)

The install script downloads the latest release, installs the binary, creates a starter config, and sets up the systemd service:

```bash
curl -fsSL https://raw.githubusercontent.com/eugenetaranov/cronlock/main/scripts/install.sh | sudo bash
```

### Build from Source

```bash
# Build binary to bin/
make build

# Or install to $GOPATH/bin
make install
```

## Quick Start

1. Copy and edit the example configuration:

```bash
cp configs/cronlock.example.yaml cronlock.yaml
# Edit cronlock.yaml with your jobs and Redis settings
```

2. Validate your configuration:

```bash
./bin/cronlock -validate -config cronlock.yaml
```

3. Start cronlock:

```bash
./bin/cronlock -config cronlock.yaml
```

## Command Line Options

```
-config string    Path to configuration file (default "cronlock.yaml")
-validate         Validate configuration and exit (exit 0 on success, 1 on failure)
-version          Show version and exit
```

**Version output format:**
- Tagged release: `cronlock v1.0.0 (abc1234)`
- Development build: `cronlock abc1234`

## Configuration

Cronlock supports YAML and TOML configuration formats. See `configs/` for examples.

**Validation** (performed at startup and with `-validate`):
- Cron schedule syntax is validated before the scheduler starts
- Redis DB must be 0-15
- Duration fields (`timeout`, `lock_ttl`, `grace_period`) must be non-negative
- Job names must be unique

### Node Configuration

```yaml
node:
  id: "node-1"           # Unique node identifier (auto-generated if not set)
  grace_period: 5s       # Wait time after job completion before releasing lock
```

### Redis Configuration

```yaml
redis:
  address: "localhost:6379"
  password: ""           # Optional
  db: 0
  key_prefix: "cronlock:"
```

### Job Configuration

```yaml
jobs:
  - name: "backup"           # Unique job name
    schedule: "0 2 * * *"    # Cron expression
    command: "/path/to/script.sh"
    timeout: 1h              # Max execution time (optional)
    lock_ttl: 2h             # Lock duration (defaults to timeout + 1min)
    work_dir: "/var/backups" # Working directory (optional)
    enabled: true            # Enable/disable job (default: true)
    env:                     # Environment variables (optional)
      KEY: "value"
    on_success: "notify.sh"  # Command to run on success (optional)
    on_failure: "alert.sh"   # Command to run on failure (optional)
```

### Schedule Format

Standard cron expressions are supported:

```
┌───────────── second (optional, 0-59)
│ ┌───────────── minute (0-59)
│ │ ┌───────────── hour (0-23)
│ │ │ ┌───────────── day of month (1-31)
│ │ │ │ ┌───────────── month (1-12)
│ │ │ │ │ ┌───────────── day of week (0-6, Sunday=0)
│ │ │ │ │ │
* * * * * *
```

Special expressions:
- `@yearly` / `@annually` - Once a year
- `@monthly` - Once a month
- `@weekly` - Once a week
- `@daily` / `@midnight` - Once a day
- `@hourly` - Once an hour
- `@every <duration>` - Every interval (e.g., `@every 1h30m`)

## Locking Strategy

1. **Key format**: `{prefix}job:{name}` (e.g., `cronlock:job:backup`)
2. **Acquire**: `SET key value NX EX ttl` (atomic)
3. **Value**: `nodeID:uuid` to ensure only the owner can release
4. **Renewal**: Every TTL/3 for long-running jobs
5. **Release**: Lua script for atomic check-and-delete
6. **Grace period**: Configurable delay after completion before release

## Systemd Integration

The install script (`scripts/install.sh`) automatically sets up the systemd service. After installation:

```bash
# Edit config with your jobs
sudo vi /etc/cronlock/cronlock.yaml

# Enable and start
sudo systemctl enable --now cronlock

# Check status
sudo systemctl status cronlock
```

For manual installation:

```bash
# Copy binary
sudo cp bin/cronlock /usr/local/bin/

# Copy and edit config
sudo mkdir -p /etc/cronlock
sudo cp configs/cronlock.example.yaml /etc/cronlock/cronlock.yaml

# Install and enable service
sudo cp scripts/cronlock.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable cronlock
sudo systemctl start cronlock
```

## High Availability

Run multiple instances of cronlock with the same configuration:

```bash
# Node 1
./cronlock --config cronlock.yaml

# Node 2 (different server)
./cronlock --config cronlock.yaml

# Node 3 (different server)
./cronlock --config cronlock.yaml
```

Each job will be executed by exactly one node. If a node fails, another will take over on the next scheduled run.

## Development

```bash
# Run tests
make test

# Run with coverage
make test-cover

# Format code
make fmt

# Run linter
make lint
```

## License

MIT
