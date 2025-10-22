package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/fabio/go-magento-cron-monitor/internal/analyzer"
	"github.com/fabio/go-magento-cron-monitor/internal/config"
	"github.com/fabio/go-magento-cron-monitor/internal/database"
	"github.com/fabio/go-magento-cron-monitor/internal/logger"
)

// Service manages the monitoring loop
type Service struct {
	config    *config.Config
	db        *database.Client
	logger    *logger.Logger
	analyzer  *analyzer.Analyzer
	verbosity int
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewService creates a new monitor service
func NewService(cfg *config.Config, db *database.Client, log *logger.Logger, verbosity int) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		config:    cfg,
		db:        db,
		logger:    log,
		analyzer:  analyzer.NewAnalyzer(cfg),
		verbosity: verbosity,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start begins the monitoring loop
func (s *Service) Start() error {
	s.logger.Info("Monitor service started", nil)
	s.logger.Info("Monitoring ticker interval", map[string]interface{}{
		"interval": s.config.Monitor.Interval.String(),
	})

	ticker := time.NewTicker(s.config.Monitor.Interval)
	defer ticker.Stop()

	// Run initial check immediately
	if err := s.runCheck(); err != nil {
		s.logger.Error("Initial check failed", err, nil)
	}

	// Main monitoring loop
	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debug("Monitor service stopping...", nil)
			return nil

		case <-ticker.C:
			if err := s.runCheck(); err != nil {
				s.logger.Error("Check failed", err, nil)
			}
		}
	}
}

// Stop gracefully stops the monitoring service
func (s *Service) Stop() {
	s.cancel()
}

// runCheck performs a single monitoring check
func (s *Service) runCheck() error {
	s.logger.Debug("Running cron check...", nil)

	start := time.Now()

	// Fetch recent cron schedules
	schedules, err := s.db.GetRecentCronSchedules(s.config.Monitor.Detection.LookbackWindow)
	if err != nil {
		return fmt.Errorf("failed to fetch cron schedules: %w", err)
	}

	s.logger.Debug("Fetched cron schedules", map[string]interface{}{
		"count":    len(schedules),
		"duration": time.Since(start).String(),
	})

	// Analyze for stuck crons
	alerts := s.analyzer.Analyze(schedules)

	// Log alerts
	for _, alert := range alerts {
		s.logger.LogStuckCron(alert)
	}

	// Log summary
	s.logCheckSummary(schedules, alerts, time.Since(start))

	return nil
}

// logCheckSummary logs a summary of the check results
func (s *Service) logCheckSummary(schedules []*database.CronSchedule, alerts []*logger.StuckCronAlert, duration time.Duration) {
	// Count by status
	statusCounts := make(map[string]int)
	for _, sched := range schedules {
		statusCounts[sched.Status]++
	}

	// Count unique jobs
	uniqueJobs := make(map[string]bool)
	for _, sched := range schedules {
		uniqueJobs[sched.JobCode] = true
	}

	fields := map[string]interface{}{
		"total_records": len(schedules),
		"unique_jobs":   len(uniqueJobs),
		"alerts":        len(alerts),
		"duration":      duration.String(),
	}

	for status, count := range statusCounts {
		fields[fmt.Sprintf("status_%s", status)] = count
	}

	if len(alerts) > 0 {
		s.logger.Info("Check completed with alerts", fields)
	} else {
		s.logger.Info("Check completed - all healthy", fields)
	}

	// Log detailed job states at debug level
	s.logJobStates()
}

// logJobStates logs current job states (debug level)
func (s *Service) logJobStates() {
	states := s.analyzer.GetJobStates()
	if len(states) == 0 {
		return
	}

	s.logger.Debug("Current job states", map[string]interface{}{"count": len(states)})

	for jobCode, state := range states {
		if state.ConsecutiveStuck > 0 || state.ErrorStreak > 0 || state.MissedStreak > 0 {
			s.logger.Debug("Job state", map[string]interface{}{
				"job_code":          jobCode,
				"cron_group":        state.CronGroup,
				"consecutive_stuck": state.ConsecutiveStuck,
				"error_streak":      state.ErrorStreak,
				"missed_streak":     state.MissedStreak,
				"last_status":       state.LastStatus,
			})
		}
	}
}
