package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config represents the application configuration
type Config struct {
	Database DatabaseConfig `mapstructure:"database"`
	Monitor  MonitorConfig  `mapstructure:"monitor"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

// DatabaseConfig holds database connection settings
type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Name     string `mapstructure:"name"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
}

// MonitorConfig holds monitoring settings
type MonitorConfig struct {
	Interval  time.Duration     `mapstructure:"interval"`
	Detection DetectionConfig   `mapstructure:"detection"`
	CronGroups []CronGroupConfig `mapstructure:"cron_groups"`
}

// DetectionConfig holds global detection thresholds
type DetectionConfig struct {
	MaxRunningTime     time.Duration `mapstructure:"max_running_time"`
	MaxPendingCount    int           `mapstructure:"max_pending_count"`
	ConsecutiveErrors  int           `mapstructure:"consecutive_errors"`
	MaxMissedCount     int           `mapstructure:"max_missed_count"`
	LookbackWindow     time.Duration `mapstructure:"lookback_window"`
	ThresholdChecks    int           `mapstructure:"threshold_checks"`      // Consecutive checks before alerting
	
	// Scheduler health check settings
	SchedulerInactivityMinutes int `mapstructure:"scheduler_inactivity_minutes"` // No new jobs created in X minutes
	SchedulerLookaheadMinutes  int `mapstructure:"scheduler_lookahead_minutes"`  // No pending jobs scheduled in next X minutes
}

// CronGroupConfig holds per-group configuration overrides
type CronGroupConfig struct {
	Name               string         `mapstructure:"name"`
	CheckInterval      *time.Duration `mapstructure:"check_interval"`       // Optional override
	MaxRunningTime     *time.Duration `mapstructure:"max_running_time"`
	MaxPendingCount    *int           `mapstructure:"max_pending_count"`
	ConsecutiveErrors  *int           `mapstructure:"consecutive_errors"`
	MaxMissedCount     *int           `mapstructure:"max_missed_count"`
	ThresholdChecks    *int           `mapstructure:"threshold_checks"`
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	File   string `mapstructure:"file"`
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"` // json or text
}

// Load reads and parses the configuration file
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config file
	v.SetConfigFile(configPath)

	// Enable environment variable support
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read the config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables in password field
	if password := v.GetString("database.password"); strings.HasPrefix(password, "${") && strings.HasSuffix(password, "}") {
		envVar := strings.TrimSuffix(strings.TrimPrefix(password, "${"), "}")
		v.Set("database.password", os.Getenv(envVar))
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Set defaults
	if cfg.Monitor.Interval == 0 {
		cfg.Monitor.Interval = 2 * time.Minute
	}
	if cfg.Monitor.Detection.MaxRunningTime == 0 {
		cfg.Monitor.Detection.MaxRunningTime = 30 * time.Minute
	}
	if cfg.Monitor.Detection.MaxPendingCount == 0 {
		cfg.Monitor.Detection.MaxPendingCount = 20
	}
	if cfg.Monitor.Detection.ConsecutiveErrors == 0 {
		cfg.Monitor.Detection.ConsecutiveErrors = 3
	}
	if cfg.Monitor.Detection.MaxMissedCount == 0 {
		cfg.Monitor.Detection.MaxMissedCount = 5
	}
	if cfg.Monitor.Detection.LookbackWindow == 0 {
		cfg.Monitor.Detection.LookbackWindow = 1 * time.Hour
	}
	if cfg.Monitor.Detection.ThresholdChecks == 0 {
		cfg.Monitor.Detection.ThresholdChecks = 2
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Database.Port == 0 {
		cfg.Database.Port = 3306
	}

	// Validate
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if cfg.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if cfg.Database.Name == "" {
		return fmt.Errorf("database.name is required")
	}
	if cfg.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}
	if cfg.Logging.File == "" {
		return fmt.Errorf("logging.file is required")
	}
	if cfg.Logging.Format != "json" && cfg.Logging.Format != "text" {
		return fmt.Errorf("logging.format must be 'json' or 'text'")
	}
	return nil
}

// GetCheckInterval returns the effective check interval for a cron group
func (c *CronGroupConfig) GetCheckInterval(defaultInterval time.Duration) time.Duration {
	if c.CheckInterval != nil {
		return *c.CheckInterval
	}
	return defaultInterval
}

// GetDetectionConfig returns the effective detection config for a cron group
// It merges group-specific overrides with global defaults
func (c *Config) GetDetectionConfig(groupName string) DetectionConfig {
	cfg := c.Monitor.Detection // Start with global defaults

	// Find group-specific config
	for _, group := range c.Monitor.CronGroups {
		if group.Name == groupName {
			// Apply overrides
			if group.MaxRunningTime != nil {
				cfg.MaxRunningTime = *group.MaxRunningTime
			}
			if group.MaxPendingCount != nil {
				cfg.MaxPendingCount = *group.MaxPendingCount
			}
			if group.ConsecutiveErrors != nil {
				cfg.ConsecutiveErrors = *group.ConsecutiveErrors
			}
			if group.MaxMissedCount != nil {
				cfg.MaxMissedCount = *group.MaxMissedCount
			}
			if group.ThresholdChecks != nil {
				cfg.ThresholdChecks = *group.ThresholdChecks
			}
			break
		}
	}

	return cfg
}
