# Magento 2 Cron Monitor

A Go CLI application that continuously monitors Magento 2 cron jobs by querying the `cron_schedule` database table to detect stuck or problematic cron jobs and logs alerts to a file.

## Features

- 🔍 **Continuous Monitoring** - Periodically checks cron status at configurable intervals
- 📊 **Smart Detection** - Uses multiple criteria to identify stuck cron jobs:
  - Long-running jobs
  - Accumulating pending jobs
  - Consecutive errors
  - Missed executions
  - Threshold-based detection
- ⚙️ **Flexible Configuration** - YAML-based configuration with per-group overrides
- 📝 **Structured Logging** - JSON or text format logging to file and stdout
- 🎯 **Selective Monitoring** - Configure different thresholds for different cron groups

## Installation

### Prerequisites

- Go 1.21 or higher
- MySQL/MariaDB database with Magento 2 schema
- Access to `cron_schedule` table

### Build from Source

```bash
# Clone the repository
cd /path/to/go-magento-cron-monitor

# Download dependencies
go mod download

# Build the application
go build -o go-magento-cron-monitor

# Optional: Install to system path
sudo mv go-magento-cron-monitor /usr/local/bin/
```

## Configuration

Create a `config.yaml` file with your database connection details and monitoring preferences:

```yaml
database:
  host: "localhost"
  port: 3306
  name: "magento"
  user: "magento_user"
  password: "your_password"  # Supports ${ENV_VAR} syntax
  
monitor:
  # How often to check cron status
  interval: 2m
  
  detection:
    # Consecutive checks before flagging as stuck
    threshold_checks: 2
    # Maximum time a job should run
    max_running_time: 30m
    # Maximum pending jobs before alerting
    max_pending_count: 20
    # Consecutive errors before alerting
    consecutive_errors: 3
    # Maximum missed executions
    max_missed_count: 5
    # How far back to look in cron_schedule table
    lookback_window: 1h
  
  # Optional: per-group overrides
  cron_groups:
    - name: "index"
      max_running_time: 60m
      max_pending_count: 10
      
    - name: "consumers"
      max_running_time: 15m

logging:
  file: "./logs/magento-cron-monitor.log"
  level: "info"  # debug, info, warn, error
  format: "json"  # json or text
```

### Configuration Options

#### Database Settings

- `host` - MySQL/MariaDB server hostname
- `port` - Database port (default: 3306)
- `name` - Database name
- `user` - Database username
- `password` - Database password (supports `${ENV_VAR}` syntax)

#### Monitor Settings

- `interval` - How often to check cron jobs (e.g., `60s`, `2m`, `5m`)
- `detection.threshold_checks` - Consecutive checks before alerting (reduces false positives)
- `detection.max_running_time` - Alert if job runs longer than this
- `detection.max_pending_count` - Alert if more pending jobs than this threshold
- `detection.consecutive_errors` - Alert after this many consecutive errors
- `detection.max_missed_count` - Alert if job missed this many times in lookback window
- `detection.lookback_window` - Time range to query from `cron_schedule` table
- `cron_groups` - Per-group overrides (auto-detected from job_code patterns)

#### Logging Settings

- `file` - Path to log file (directory will be created if needed)
- `level` - Log level: `debug`, `info`, `warn`, `error`
- `format` - Log format: `json` or `text`

## Usage

```bash
# Use default config.yaml in current directory
./go-magento-cron-monitor monitor

# Use custom config file
./go-magento-cron-monitor monitor --config /path/to/config.yaml
```

## Deployment

### Systemd Service

Create `/etc/systemd/system/magento-cron-monitor.service`:

```ini
[Unit]
Description=Magento 2 Cron Monitor
After=network.target mysql.service

[Service]
Type=simple
User=magento
WorkingDirectory=/opt/magento-cron-monitor
ExecStart=/opt/magento-cron-monitor/go-magento-cron-monitor monitor --config /etc/magento-cron-monitor/config.yaml
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable magento-cron-monitor
sudo systemctl start magento-cron-monitor
sudo systemctl status magento-cron-monitor
```

### Docker

Create a `Dockerfile`:

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o go-magento-cron-monitor

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/go-magento-cron-monitor .
COPY config.yaml .
CMD ["./go-magento-cron-monitor", "monitor"]
```

Build and run:
```bash
docker build -t magento-cron-monitor .
docker run -v $(pwd)/config.yaml:/root/config.yaml magento-cron-monitor
```