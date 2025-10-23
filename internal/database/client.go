package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/fabio/go-magento-cron-monitor/internal/config"
)

// CronSchedule represents a row from the cron_schedule table
type CronSchedule struct {
	ScheduleID  int
	JobCode     string
	Status      string
	Messages    sql.NullString
	CreatedAt   time.Time
	ScheduledAt time.Time
	ExecutedAt  sql.NullTime
	FinishedAt  sql.NullTime
}

// Client wraps database operations
type Client struct {
	db *sql.DB
}

// NewClient creates a new database client
func NewClient(cfg config.DatabaseConfig) (*Client, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Name,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	return c.db.Close()
}

// Ping tests the database connection
func (c *Client) Ping() error {
	return c.db.Ping()
}

// GetCronScheduleCount returns the total number of cron_schedule records
func (c *Client) GetCronScheduleCount() (int, error) {
	var count int
	err := c.db.QueryRow("SELECT COUNT(*) FROM cron_schedule").Scan(&count)
	return count, err
}

// GetRecentCronSchedules retrieves cron schedules within the lookback window
func (c *Client) GetRecentCronSchedules(lookbackWindow time.Duration) ([]*CronSchedule, error) {
	cutoffTime := time.Now().Add(-lookbackWindow)

	query := `
		SELECT 
			schedule_id,
			job_code,
			status,
			messages,
			created_at,
			scheduled_at,
			executed_at,
			finished_at
		FROM cron_schedule
		WHERE created_at >= ?
		ORDER BY created_at DESC
	`

	rows, err := c.db.Query(query, cutoffTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query cron_schedule: %w", err)
	}
	defer rows.Close()

	var schedules []*CronSchedule
	for rows.Next() {
		var s CronSchedule
		err := rows.Scan(
			&s.ScheduleID,
			&s.JobCode,
			&s.Status,
			&s.Messages,
			&s.CreatedAt,
			&s.ScheduledAt,
			&s.ExecutedAt,
			&s.FinishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		schedules = append(schedules, &s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return schedules, nil
}

// GetRunningCronJobs retrieves all cron jobs currently in running status
func (c *Client) GetRunningCronJobs() ([]*CronSchedule, error) {
	query := `
		SELECT 
			schedule_id,
			job_code,
			status,
			messages,
			created_at,
			scheduled_at,
			executed_at,
			finished_at
		FROM cron_schedule
		WHERE status = 'running'
		ORDER BY executed_at ASC
	`

	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query running cron jobs: %w", err)
	}
	defer rows.Close()

	var schedules []*CronSchedule
	for rows.Next() {
		var s CronSchedule
		err := rows.Scan(
			&s.ScheduleID,
			&s.JobCode,
			&s.Status,
			&s.Messages,
			&s.CreatedAt,
			&s.ScheduledAt,
			&s.ExecutedAt,
			&s.FinishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		schedules = append(schedules, &s)
	}

	return schedules, nil
}

// GetJobHistory retrieves recent history for a specific job code
func (c *Client) GetJobHistory(jobCode string, lookbackWindow time.Duration, limit int) ([]*CronSchedule, error) {
	cutoffTime := time.Now().Add(-lookbackWindow)

	query := `
		SELECT 
			schedule_id,
			job_code,
			status,
			messages,
			created_at,
			scheduled_at,
			executed_at,
			finished_at
		FROM cron_schedule
		WHERE job_code = ? AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT ?
	`

	rows, err := c.db.Query(query, jobCode, cutoffTime, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query job history: %w", err)
	}
	defer rows.Close()

	var schedules []*CronSchedule
	for rows.Next() {
		var s CronSchedule
		err := rows.Scan(
			&s.ScheduleID,
			&s.JobCode,
			&s.Status,
			&s.Messages,
			&s.CreatedAt,
			&s.ScheduledAt,
			&s.ExecutedAt,
			&s.FinishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		schedules = append(schedules, &s)
	}

	return schedules, nil
}

// GetPendingJobCounts returns count of pending jobs grouped by job_code
func (c *Client) GetPendingJobCounts() (map[string]int, error) {
	query := `
		SELECT job_code, COUNT(*) as count
		FROM cron_schedule
		WHERE status = 'pending'
		GROUP BY job_code
	`

	rows, err := c.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending job counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var jobCode string
		var count int
		if err := rows.Scan(&jobCode, &count); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		counts[jobCode] = count
	}

	return counts, nil
}

// GetRecentlyCreatedJobCount returns count of jobs created within the specified time window
func (c *Client) GetRecentlyCreatedJobCount(minutes int) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM cron_schedule 
		WHERE created_at >= DATE_SUB(NOW(), INTERVAL ? MINUTE)
	`

	var count int
	err := c.db.QueryRow(query, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to query recently created jobs: %w", err)
	}

	return count, nil
}

// GetUpcomingPendingJobCount returns count of pending jobs scheduled in the near future
func (c *Client) GetUpcomingPendingJobCount(minutes int) (int, error) {
	query := `
		SELECT COUNT(*) 
		FROM cron_schedule 
		WHERE status = 'pending' 
		AND scheduled_at BETWEEN NOW() AND DATE_ADD(NOW(), INTERVAL ? MINUTE)
	`

	var count int
	err := c.db.QueryRow(query, minutes).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to query upcoming pending jobs: %w", err)
	}

	return count, nil
}
