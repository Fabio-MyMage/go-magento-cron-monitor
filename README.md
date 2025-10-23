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
- 🚦 **Scheduler Health** - Detects if the Magento cron scheduler process (`php bin/magento cron:run`) has stopped
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
    
    # Scheduler health detection
    scheduler_inactivity_minutes: 10
    scheduler_lookahead_minutes: 15
  
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
- `detection.scheduler_inactivity_minutes` - Alert if no jobs created in this timeframe (default: 10)
- `detection.scheduler_lookahead_minutes` - AND no pending jobs scheduled in next X minutes (default: 15)
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

## Detection Criteria

### Stuck Cron Jobs

The monitor checks for several types of problems with individual cron jobs:

1. **Long-Running Jobs** - Jobs that have been in `running` status longer than `max_running_time`
2. **Pending Accumulation** - More than `max_pending_count` jobs with `pending` status for the same job code
3. **Consecutive Errors** - Job has failed `consecutive_errors` times in a row
4. **Missed Executions** - Job has `missed` status more than `max_missed_count` times within `lookback_window`

All detections use threshold-based alerting: the condition must be detected `threshold_checks` consecutive times before an alert is logged. This reduces false positives from transient issues.

### Scheduler Health (STUCK CRON SCHEDULER)

In addition to monitoring individual cron jobs, the monitor also checks if the Magento cron scheduler process itself (`php bin/magento cron:run`) is running. This is critical because if the scheduler stops, jobs won't be created or executed even though they may appear "healthy" in the database.

The scheduler is considered stuck when **BOTH** conditions are true:
1. No new jobs have been created in the last `scheduler_inactivity_minutes` (default: 10 minutes)
2. No pending jobs are scheduled for the next `scheduler_lookahead_minutes` (default: 15 minutes)

This dual-check approach prevents false positives during normal periods of low cron activity. The alert will be logged as:

```json
{
  "timestamp": "2025-01-22T09:59:00Z",
  "level": "ERROR",
  "message": "STUCK CRON SCHEDULER",
  "job_code": "SCHEDULER",
  "group": "system",
  "reason": "No jobs created in 10 minutes and no pending jobs scheduled for next 15 minutes"
}
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