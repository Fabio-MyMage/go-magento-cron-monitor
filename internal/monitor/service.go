package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/fabio/go-magento-cron-monitor/internal/analyzer"
	"github.com/fabio/go-magento-cron-monitor/internal/config"
	"github.com/fabio/go-magento-cron-monitor/internal/database"
	"github.com/fabio/go-magento-cron-monitor/internal/logger"
	"github.com/fabio/go-magento-cron-monitor/internal/slack"
)

// Service manages the monitoring loop
type Service struct {
	config      *config.Config
	db          *database.Client
	logger      *logger.Logger
	analyzer    *analyzer.Analyzer
	slackClient *slack.Client
	verbosity   int
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewService creates a new monitor service
func NewService(cfg *config.Config, db *database.Client, log *logger.Logger, verbosity int) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	// Create Slack client if enabled
	var slackClient *slack.Client
	if cfg.Notifications.Slack.Enabled {
		slackConfig := slack.Config{
			Enabled:          cfg.Notifications.Slack.Enabled,
			WebhookURLs:      cfg.Notifications.Slack.WebhookURLs,
			AlertCooldown:    cfg.Notifications.Slack.AlertCooldown,
			SendRecovery:     cfg.Notifications.Slack.SendRecovery,
			RecoveryCooldown: cfg.Notifications.Slack.RecoveryCooldown,
			Timeout:          cfg.Notifications.Slack.Timeout,
		}
		slackClient = slack.New(slackConfig)
		log.Info("Slack notifications enabled", map[string]interface{}{
			"webhook_count":     len(slackConfig.WebhookURLs),
			"alert_cooldown":    slackConfig.AlertCooldown.String(),
			"send_recovery":     slackConfig.SendRecovery,
			"recovery_cooldown": slackConfig.RecoveryCooldown.String(),
		})
	}

	return &Service{
		config:      cfg,
		db:          db,
		logger:      log,
		analyzer:    analyzer.NewAnalyzer(cfg),
		slackClient: slackClient,
		verbosity:   verbosity,
		ctx:         ctx,
		cancel:      cancel,
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

	// Check scheduler health
	if schedulerAlert := s.analyzer.CheckSchedulerHealth(s.db); schedulerAlert != nil {
		alerts = append(alerts, schedulerAlert)
	}

	// Log alerts
	for _, alert := range alerts {
		s.logger.LogStuckCron(alert)
	}

	// Detect state transitions for Slack notifications
	if s.slackClient != nil {
		transitions := s.analyzer.DetectStateTransitions(schedules)
		
		// Create alert lookup map for enriching transitions
		alertMap := make(map[string]*logger.StuckCronAlert)
		for _, alert := range alerts {
			alertMap[alert.JobCode] = alert
		}
		
		for _, transition := range transitions {
			// Find corresponding alert for additional details
			var enrichedAlert *logger.StuckCronAlert
			if alert, exists := alertMap[transition.CronCode]; exists {
				enrichedAlert = alert
			}
			
			if err := s.handleStateTransition(transition, time.Now(), enrichedAlert); err != nil {
				s.logger.Error("Failed to send Slack notification", err, map[string]interface{}{
					"cron_code": transition.CronCode,
				})
			}
		}
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
		s.logger.Info("Check completed - all not alerting", fields)
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

// handleStateTransition processes state transitions and sends Slack notifications
func (s *Service) handleStateTransition(transition analyzer.StateTransition, now time.Time, enrichedAlert *logger.StuckCronAlert) error {
	state := s.analyzer.GetCronState(transition.CronCode)
	if state == nil {
		return fmt.Errorf("cron state not found: %s", transition.CronCode)
	}

	// Determine cooldown based on transition type
	var cooldown time.Duration
	var alertType slack.AlertType

	if transition.ToState == "alerting" {
		// Cron became alerting
		cooldown = s.config.Notifications.Slack.AlertCooldown
		alertType = slack.AlertTypeAlerting
	} else if transition.ToState == "not_alerting" {
		// Cron recovered
		if !s.config.Notifications.Slack.SendRecovery {
			s.logger.Debug("Skipping recovery notification (disabled)", map[string]interface{}{
				"cron_code": transition.CronCode,
			})
			return nil
		}
		cooldown = s.config.Notifications.Slack.RecoveryCooldown
		alertType = slack.AlertTypeNotAlerting
	} else {
		return nil
	}

	// Check cooldown
	if !state.LastSlackAlert.IsZero() && now.Sub(state.LastSlackAlert) < cooldown {
		s.logger.Debug("Skipping Slack notification (cooldown active)", map[string]interface{}{
			"cron_code":       transition.CronCode,
			"alert_type":      string(alertType),
			"cooldown":        cooldown.String(),
			"time_since_last": now.Sub(state.LastSlackAlert).String(),
		})
		return nil
	}

	// Create Slack alert
	slackAlert := slack.CronAlert{
		Type:          alertType,
		CronCode:      transition.CronCode,
		Status:        transition.Status,
		LastExecution: transition.LastExecution,
		StuckDuration: transition.StuckDuration,
		Timestamp:     now,
	}
	
	// Enrich with detailed alert data if available
	if enrichedAlert != nil {
		slackAlert.CronGroup = enrichedAlert.CronGroup
		slackAlert.RunningTime = enrichedAlert.RunningTime
		slackAlert.ScheduledAt = enrichedAlert.ScheduledAt
		slackAlert.Reason = enrichedAlert.Reason
		slackAlert.ConsecutiveStuck = enrichedAlert.ConsecutiveStuck
		slackAlert.PendingCount = enrichedAlert.PendingCount
		slackAlert.ErrorCount = enrichedAlert.ErrorCount
		slackAlert.MissedCount = enrichedAlert.MissedCount
	}

	// Send notification
	if err := s.slackClient.SendAlert(slackAlert); err != nil {
		return err
	}

	// Update last alert time
	state.LastSlackAlert = now

	s.logger.Info("Sent Slack notification", map[string]interface{}{
		"cron_code":  transition.CronCode,
		"alert_type": string(alertType),
	})

	return nil
}
