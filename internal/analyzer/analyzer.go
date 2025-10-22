package analyzer

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fabio/go-magento-cron-monitor/internal/config"
	"github.com/fabio/go-magento-cron-monitor/internal/database"
	"github.com/fabio/go-magento-cron-monitor/internal/logger"
)

// Analyzer detects stuck cron jobs
type Analyzer struct {
	config *config.Config
	// Track state across checks
	jobStates map[string]*JobState
	mu        sync.RWMutex
}

// JobState tracks the state of a cron job across multiple checks
type JobState struct {
	JobCode          string
	CronGroup        string
	ConsecutiveStuck int
	LastStatus       string
	LastChecked      time.Time
	LastAlertTime    time.Time
	ErrorStreak      int
	MissedStreak     int
}

// NewAnalyzer creates a new analyzer
func NewAnalyzer(cfg *config.Config) *Analyzer {
	return &Analyzer{
		config:    cfg,
		jobStates: make(map[string]*JobState),
	}
}

// Analyze examines recent cron schedules and detects stuck jobs
func (a *Analyzer) Analyze(schedules []*database.CronSchedule) []*logger.StuckCronAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	var alerts []*logger.StuckCronAlert

	// Group schedules by job_code
	jobSchedules := make(map[string][]*database.CronSchedule)
	for _, s := range schedules {
		jobSchedules[s.JobCode] = append(jobSchedules[s.JobCode], s)
	}

	// Analyze each job
	for jobCode, schedList := range jobSchedules {
		cronGroup := a.extractCronGroup(jobCode)
		detectionCfg := a.config.GetDetectionConfig(cronGroup)

		// Get or create job state
		state, exists := a.jobStates[jobCode]
		if !exists {
			state = &JobState{
				JobCode:   jobCode,
				CronGroup: cronGroup,
			}
			a.jobStates[jobCode] = state
		}
		state.LastChecked = time.Now()

		// Check for various stuck conditions
		if alert := a.checkLongRunning(schedList, detectionCfg, state); alert != nil {
			// Suppress duplicate alerts within 5 minutes
			if time.Since(state.LastAlertTime) >= 5*time.Minute {
				alerts = append(alerts, alert)
				state.LastAlertTime = time.Now()
			}
		}
		if alert := a.checkPendingAccumulation(schedList, detectionCfg, state); alert != nil {
			if time.Since(state.LastAlertTime) >= 5*time.Minute {
				alerts = append(alerts, alert)
				state.LastAlertTime = time.Now()
			}
		}
		if alert := a.checkConsecutiveErrors(schedList, detectionCfg, state); alert != nil {
			if time.Since(state.LastAlertTime) >= 5*time.Minute {
				alerts = append(alerts, alert)
				state.LastAlertTime = time.Now()
			}
		}
		if alert := a.checkMissedExecutions(schedList, detectionCfg, state); alert != nil {
			if time.Since(state.LastAlertTime) >= 5*time.Minute {
				alerts = append(alerts, alert)
				state.LastAlertTime = time.Now()
			}
		}
	}

	// Clean up old job states
	a.cleanupOldStates()

	return alerts
}

// checkLongRunning detects jobs that have been running too long
func (a *Analyzer) checkLongRunning(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) *logger.StuckCronAlert {
	for _, s := range schedules {
		if s.Status != "running" {
			continue
		}

		if !s.ExecutedAt.Valid {
			continue
		}

		runningTime := time.Since(s.ExecutedAt.Time)
		if runningTime > cfg.MaxRunningTime {
			state.ConsecutiveStuck++

			// Only alert after threshold consecutive detections
			if state.ConsecutiveStuck >= cfg.ThresholdChecks {
				return &logger.StuckCronAlert{
					JobCode:          s.JobCode,
					CronGroup:        state.CronGroup,
					Status:           s.Status,
					RunningTime:      &runningTime,
					ScheduledAt:      &s.ScheduledAt,
					ExecutedAt:       &s.ExecutedAt.Time,
					Reason:           fmt.Sprintf("job running longer than max_running_time threshold (%s)", cfg.MaxRunningTime),
					ConsecutiveStuck: state.ConsecutiveStuck,
				}
			}
			return nil
		}
	}

	// Reset streak if not stuck
	state.ConsecutiveStuck = 0
	return nil
}

// checkPendingAccumulation detects too many pending jobs
func (a *Analyzer) checkPendingAccumulation(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) *logger.StuckCronAlert {
	pendingCount := 0
	for _, s := range schedules {
		if s.Status == "pending" {
			pendingCount++
		}
	}

	if pendingCount > cfg.MaxPendingCount {
		state.ConsecutiveStuck++

		if state.ConsecutiveStuck >= cfg.ThresholdChecks {
			return &logger.StuckCronAlert{
				JobCode:          state.JobCode,
				CronGroup:        state.CronGroup,
				Status:           "pending",
				PendingCount:     pendingCount,
				Reason:           fmt.Sprintf("too many pending jobs (%d > %d)", pendingCount, cfg.MaxPendingCount),
				ConsecutiveStuck: state.ConsecutiveStuck,
			}
		}
		return nil
	}

	// Reset if under threshold
	if state.LastStatus == "pending_accumulation" {
		state.ConsecutiveStuck = 0
	}
	return nil
}

// checkConsecutiveErrors detects jobs repeatedly failing
func (a *Analyzer) checkConsecutiveErrors(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) *logger.StuckCronAlert {
	// Count consecutive errors from most recent schedules
	errorCount := 0
	var lastError *database.CronSchedule

	for i := 0; i < len(schedules) && i < cfg.ConsecutiveErrors*2; i++ {
		s := schedules[i]
		if s.Status == "error" {
			errorCount++
			if lastError == nil {
				lastError = s
			}
		} else if s.Status == "success" {
			// Break streak if we hit a success
			break
		}
	}

	if errorCount >= cfg.ConsecutiveErrors {
		state.ErrorStreak = errorCount
		state.ConsecutiveStuck++

		if state.ConsecutiveStuck >= cfg.ThresholdChecks {
			alert := &logger.StuckCronAlert{
				JobCode:          state.JobCode,
				CronGroup:        state.CronGroup,
				Status:           "error",
				ErrorCount:       errorCount,
				Reason:           fmt.Sprintf("consecutive errors detected (%d >= %d)", errorCount, cfg.ConsecutiveErrors),
				ConsecutiveStuck: state.ConsecutiveStuck,
			}

			if lastError != nil && lastError.Messages.Valid {
				alert.ErrorMessage = lastError.Messages.String
				alert.ScheduledAt = &lastError.ScheduledAt
			}

			return alert
		}
		return nil
	}

	// Reset if no error streak
	state.ErrorStreak = 0
	if state.LastStatus == "consecutive_errors" {
		state.ConsecutiveStuck = 0
	}
	return nil
}

// checkMissedExecutions detects jobs frequently being missed
func (a *Analyzer) checkMissedExecutions(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) *logger.StuckCronAlert {
	missedCount := 0
	for _, s := range schedules {
		if s.Status == "missed" {
			missedCount++
		}
	}

	if missedCount >= cfg.MaxMissedCount {
		state.MissedStreak = missedCount
		state.ConsecutiveStuck++

		if state.ConsecutiveStuck >= cfg.ThresholdChecks {
			return &logger.StuckCronAlert{
				JobCode:          state.JobCode,
				CronGroup:        state.CronGroup,
				Status:           "missed",
				MissedCount:      missedCount,
				Reason:           fmt.Sprintf("too many missed executions (%d >= %d)", missedCount, cfg.MaxMissedCount),
				ConsecutiveStuck: state.ConsecutiveStuck,
			}
		}
		return nil
	}

	// Reset if under threshold
	state.MissedStreak = 0
	if state.LastStatus == "missed_executions" {
		state.ConsecutiveStuck = 0
	}
	return nil
}

// extractCronGroup extracts the cron group from job_code
// Magento cron job codes often follow patterns like:
// - indexer_* -> index group
// - consumers_* -> consumers group
// - ddg_automation_* -> ddg_automation group
// - catalog_* -> default group
func (a *Analyzer) extractCronGroup(jobCode string) string {
	// Check if there's a specific group configured for this job pattern
	for _, group := range a.config.Monitor.CronGroups {
		// Simple prefix matching
		if strings.HasPrefix(jobCode, group.Name+"_") {
			return group.Name
		}
	}

	// Try to extract group from job code pattern
	patterns := map[*regexp.Regexp]string{
		regexp.MustCompile(`^indexer_`):        "index",
		regexp.MustCompile(`^consumers_`):      "consumers",
		regexp.MustCompile(`^ddg_automation_`): "ddg_automation",
		regexp.MustCompile(`^catalog_`):        "catalog",
		regexp.MustCompile(`^backend_`):        "backend",
		regexp.MustCompile(`^sales_`):          "sales",
	}

	for pattern, group := range patterns {
		if pattern.MatchString(jobCode) {
			return group
		}
	}

	// Default group
	return "default"
}

// cleanupOldStates removes job states that haven't been checked recently
func (a *Analyzer) cleanupOldStates() {
	cutoff := time.Now().Add(-24 * time.Hour)
	for jobCode, state := range a.jobStates {
		if state.LastChecked.Before(cutoff) {
			delete(a.jobStates, jobCode)
		}
	}
}

// GetJobStates returns current job states (for debugging)
func (a *Analyzer) GetJobStates() map[string]*JobState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	states := make(map[string]*JobState)
	for k, v := range a.jobStates {
		stateCopy := *v
		states[k] = &stateCopy
	}
	return states
}
