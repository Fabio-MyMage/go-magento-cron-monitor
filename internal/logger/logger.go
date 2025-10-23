package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fabio/go-magento-cron-monitor/internal/config"
)

// Level represents log level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// Logger handles structured logging
type Logger struct {
	file      *os.File
	format    string
	level     Level
	verbosity int
	mu        sync.Mutex
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// New creates a new logger
func New(cfg config.LoggingConfig, verbosity int) (*Logger, error) {
	// Create log directory if it doesn't exist
	logDir := filepath.Dir(cfg.File)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	level := parseLevel(cfg.Level)

	return &Logger{
		file:      file,
		format:    cfg.Format,
		level:     level,
		verbosity: verbosity,
	}, nil
}

// Close closes the log file
func (l *Logger) Close() error {
	return l.file.Close()
}

func parseLevel(levelStr string) Level {
	switch levelStr {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// isStartupMessage checks if a message is a startup/shutdown message that should always display
func isStartupMessage(msg string) bool {
	startupMessages := []string{
		"Starting Magento Cron Monitor",
		"Monitor service started",
		"Monitoring ticker interval",
		"Received shutdown signal",
		"Monitor stopped",
	}
	
	for _, sm := range startupMessages {
		if msg == sm {
			return true
		}
	}
	return false
}

// log writes a log entry
func (l *Logger) log(level Level, msg string, err error, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if we should log based on level
	if level < l.level {
		return
	}

	// Check verbosity-based filtering
	// Debug messages require -vvv
	if level == LevelDebug && l.verbosity < 3 {
		return
	}
	// Info messages for check summaries require at least -v, but startup messages always show
	// We determine this by checking if it's a routine check message
	if level == LevelInfo && l.verbosity < 1 {
		// Allow startup/shutdown messages to pass through
		if !isStartupMessage(msg) {
			return
		}
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level.String(),
		Message:   msg,
		Fields:    fields,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	var output string
	if l.format == "json" {
		data, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to marshal log entry: %v\n", marshalErr)
			return
		}
		output = string(data) + "\n"
	} else {
		// Text format: [2025-10-22T06:56:00Z] info: Message {"field":"value"}
		output = l.formatText(entry)
	}

	// Write to file
	if _, writeErr := l.file.WriteString(output); writeErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to write log: %v\n", writeErr)
	}

	// Also write to stdout
	io.WriteString(os.Stdout, output)
}

// formatText formats a log entry as text
func (l *Logger) formatText(entry LogEntry) string {
	output := fmt.Sprintf("[%s] %s: %s", entry.Timestamp, entry.Level, entry.Message)

	if len(entry.Fields) > 0 {
		fieldsJSON, _ := json.Marshal(entry.Fields)
		output += fmt.Sprintf(" %s", string(fieldsJSON))
	}

	if entry.Error != "" {
		output += fmt.Sprintf(" error=%s", entry.Error)
	}

	return output + "\n"
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	l.log(LevelDebug, msg, nil, fields)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	l.log(LevelInfo, msg, nil, fields)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	l.log(LevelWarn, msg, nil, fields)
}

// Error logs an error message
func (l *Logger) Error(msg string, err error, fields map[string]interface{}) {
	l.log(LevelError, msg, err, fields)
}

// LogStuckCron logs a stuck cron alert with all relevant details
func (l *Logger) LogStuckCron(alert *StuckCronAlert) {
	fields := map[string]interface{}{
		"job_code":          alert.JobCode,
		"cron_group":        alert.CronGroup,
		"status":            alert.Status,
		"reason":            alert.Reason,
		"consecutive_stuck": alert.ConsecutiveStuck,
	}

	if alert.RunningTime != nil {
		fields["running_time"] = alert.RunningTime.String()
	}
	if alert.ScheduledAt != nil {
		fields["scheduled_at"] = alert.ScheduledAt.Format(time.RFC3339)
	}
	if alert.ExecutedAt != nil {
		fields["executed_at"] = alert.ExecutedAt.Format(time.RFC3339)
	}
	if alert.PendingCount > 0 {
		fields["pending_count"] = alert.PendingCount
	}
	if alert.ErrorCount > 0 {
		fields["error_count"] = alert.ErrorCount
	}
	if alert.MissedCount > 0 {
		fields["missed_count"] = alert.MissedCount
	}
	if alert.ErrorMessage != "" {
		fields["error_message"] = alert.ErrorMessage
	}

	// Use different message for scheduler alerts
	message := "STUCK CRON DETECTED"
	if alert.JobCode == "SCHEDULER" {
		message = "STUCK CRON SCHEDULER"
	}

	l.log(LevelWarn, message, nil, fields)
}

// StuckCronAlert represents a stuck cron alert
type StuckCronAlert struct {
	JobCode          string
	CronGroup        string
	Status           string
	RunningTime      *time.Duration
	ScheduledAt      *time.Time
	ExecutedAt       *time.Time
	Reason           string
	ConsecutiveStuck int
	PendingCount     int
	ErrorCount       int
	MissedCount      int
	ErrorMessage     string
}
