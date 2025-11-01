package analyzer

import (
	"fmt"
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
	jobStates        map[string]*JobState
	schedulerState   *SchedulerState
	mu               sync.RWMutex
}

// JobState tracks the state of a cron job across multiple checks
type JobState struct {
	JobCode          string
	ConsecutiveStuck int
	LastStatus       string
	LastChecked      time.Time
	LastAlertTime    time.Time
	ErrorStreak      int
	MissedStreak     int
	// Slack notification tracking
	LastSlackAlert time.Time // Track last Slack notification time
	LastKnownState string    // "not_alerting" or "alerting"
	StuckSince     time.Time // When cron became stuck
}

// SchedulerState tracks the cron scheduler health across checks
type SchedulerState struct {
	ConsecutiveInactive int
	LastAlertTime       time.Time
}

// StateTransition represents a cron state change
type StateTransition struct {
	CronCode      string
	FromState     string // "not_alerting" or "alerting"
	ToState       string // "not_alerting" or "alerting"
	Timestamp     time.Time
	StuckDuration time.Duration // For alerting→not_alerting transitions
	Status        string
	LastExecution time.Time
	
	// Enhanced fields for detailed Slack alerts
	RunningTime      *time.Duration
	ScheduledAt      *time.Time
	Reason           string
	ConsecutiveStuck int
	PendingCount     int
	ErrorCount       int
	MissedCount      int
}

// NewAnalyzer creates a new analyzer
func NewAnalyzer(cfg *config.Config) *Analyzer {
	return &Analyzer{
		config:         cfg,
		jobStates:      make(map[string]*JobState),
		schedulerState: &SchedulerState{},
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
		detectionCfg := a.config.GetDetectionConfig(jobCode)

		// Get or create job state
		state, exists := a.jobStates[jobCode]
		if !exists {
			state = &JobState{
				JobCode: jobCode,
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
				Status:           "pending",
				PendingCount:     pendingCount,
				Reason:           fmt.Sprintf("too many pending jobs (%d exceeds threshold of %d)", pendingCount, cfg.MaxPendingCount),
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
				Status:           "error",
				ErrorCount:       errorCount,
				Reason:           fmt.Sprintf("consecutive errors detected (%d meets threshold of %d)", errorCount, cfg.ConsecutiveErrors),
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
				Status:           "missed",
				MissedCount:      missedCount,
				Reason:           fmt.Sprintf("too many missed executions (%d exceeds threshold of %d)", missedCount, cfg.MaxMissedCount),
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

// CheckSchedulerHealth checks if the Magento cron scheduler is running
func (a *Analyzer) CheckSchedulerHealth(dbClient *database.Client) *logger.StuckCronAlert {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	cfg := a.config.Monitor.Detection
	
	// Use defaults if not configured
	inactivityMinutes := cfg.SchedulerInactivityMinutes
	if inactivityMinutes == 0 {
		inactivityMinutes = 10 // Default: no new jobs in 10 minutes
	}
	
	lookaheadMinutes := cfg.SchedulerLookaheadMinutes
	if lookaheadMinutes == 0 {
		lookaheadMinutes = 15 // Default: no pending jobs scheduled for next 15 minutes
	}
	
	thresholdChecks := cfg.ThresholdChecks
	if thresholdChecks == 0 {
		thresholdChecks = 2
	}
	
	// Check 1: Any jobs created recently?
	recentCount, err := dbClient.GetRecentlyCreatedJobCount(inactivityMinutes)
	if err != nil {
		// Don't alert on query errors
		return nil
	}
	
	// Check 2: Any pending jobs scheduled for near future?
	upcomingCount, err := dbClient.GetUpcomingPendingJobCount(lookaheadMinutes)
	if err != nil {
		// Don't alert on query errors
		return nil
	}
	
	// Scheduler is healthy if either check passes
	if recentCount > 0 || upcomingCount > 0 {
		// Reset consecutive counter
		a.schedulerState.ConsecutiveInactive = 0
		return nil
	}
	
	// Scheduler appears inactive
	a.schedulerState.ConsecutiveInactive++
	
	// Only alert after threshold consecutive detections
	if a.schedulerState.ConsecutiveInactive < thresholdChecks {
		return nil
	}
	
	// Suppress duplicate alerts within 5 minutes
	if time.Since(a.schedulerState.LastAlertTime) < 5*time.Minute {
		return nil
	}
	
	a.schedulerState.LastAlertTime = time.Now()
	
	return &logger.StuckCronAlert{
		JobCode:          "SCHEDULER",
		Status:           "inactive",
		Reason:           fmt.Sprintf("no jobs created in last %d minutes and no pending jobs scheduled for next %d minutes", inactivityMinutes, lookaheadMinutes),
		ConsecutiveStuck: a.schedulerState.ConsecutiveInactive,
	}
}

// GetCronState returns the state for a specific cron job
func (a *Analyzer) GetCronState(cronCode string) *JobState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.jobStates[cronCode]
}

// DetectStateTransitions detects state transitions for Slack notifications
// This should be called after Analyze() to detect healthy/stuck transitions
func (a *Analyzer) DetectStateTransitions(schedules []*database.CronSchedule) []StateTransition {
	a.mu.Lock()
	defer a.mu.Unlock()

	transitions := make([]StateTransition, 0)

	// Group schedules by job_code
	jobSchedules := make(map[string][]*database.CronSchedule)
	for _, s := range schedules {
		jobSchedules[s.JobCode] = append(jobSchedules[s.JobCode], s)
	}

	// Check each job for state transitions
	for jobCode, schedList := range jobSchedules {
		state := a.jobStates[jobCode]
		if state == nil {
			continue
		}

		detectionCfg := a.config.GetDetectionConfig(jobCode)

		// Determine if currently not alerting or alerting
		isNotAlerting := a.isJobHealthy(schedList, detectionCfg, state)

		// Initialize state if empty
		if state.LastKnownState == "" {
			state.LastKnownState = "not_alerting"
		}

		// Detect not_alerting → alerting transition
		if !isNotAlerting && state.LastKnownState == "not_alerting" {
			state.StuckSince = time.Now()

			// Get last execution time and enhanced data from schedules
			var lastExec time.Time
			var scheduledAt *time.Time
			var runningTime *time.Duration
			var currentStatus string
			
			for _, s := range schedList {
				if s.ExecutedAt.Valid && (lastExec.IsZero() || s.ExecutedAt.Time.After(lastExec)) {
					lastExec = s.ExecutedAt.Time
				}
				if scheduledAt == nil || s.ScheduledAt.After(*scheduledAt) {
					scheduledAt = &s.ScheduledAt
				}
				// Calculate running time for running jobs
				if s.Status == "running" && s.ExecutedAt.Valid {
					runtime := time.Since(s.ExecutedAt.Time)
					runningTime = &runtime
					currentStatus = s.Status
				}
				if currentStatus == "" {
					currentStatus = s.Status
				}
			}

			// Get the actual reason from the alert detection methods
			reason := a.getActualAlertReason(schedList, detectionCfg, state)

			transitions = append(transitions, StateTransition{
				CronCode:         jobCode,
				FromState:        "not_alerting",
				ToState:          "alerting",
				Timestamp:        time.Now(),
				Status:           currentStatus,
				LastExecution:    lastExec,
				RunningTime:      runningTime,
				ScheduledAt:      scheduledAt,
				Reason:           reason,
				ConsecutiveStuck: state.ConsecutiveStuck,
			})
			state.LastKnownState = "alerting"
		}

		// Detect alerting → not_alerting transition
		if isNotAlerting && state.LastKnownState == "alerting" {
			duration := time.Since(state.StuckSince)

			// Get last execution time and enhanced data from schedules
			var lastExec time.Time
			var scheduledAt *time.Time
			var currentStatus string
			
			for _, s := range schedList {
				if s.ExecutedAt.Valid && (lastExec.IsZero() || s.ExecutedAt.Time.After(lastExec)) {
					lastExec = s.ExecutedAt.Time
				}
				if scheduledAt == nil || s.ScheduledAt.After(*scheduledAt) {
					scheduledAt = &s.ScheduledAt
				}
				if currentStatus == "" {
					currentStatus = s.Status
				}
			}

			transitions = append(transitions, StateTransition{
				CronCode:         jobCode,
				FromState:        "alerting",
				ToState:          "not_alerting",
				Timestamp:        time.Now(),
				StuckDuration:    duration,
				Status:           currentStatus,
				LastExecution:    lastExec,
				ScheduledAt:      scheduledAt,
				Reason:           "", // No specific reason needed for recovery
				ConsecutiveStuck: 0, // Reset since it's no longer alerting
			})
			state.LastKnownState = "not_alerting"
			state.StuckSince = time.Time{}
		}
	}

	return transitions
}

// isJobHealthy determines if a job is currently healthy (not stuck)
func (a *Analyzer) isJobHealthy(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) bool {
	// Check if any stuck condition is met
	if a.checkLongRunning(schedules, cfg, state) != nil {
		return false
	}
	if a.checkPendingAccumulation(schedules, cfg, state) != nil {
		return false
	}
	if a.checkConsecutiveErrors(schedules, cfg, state) != nil {
		return false
	}
	if a.checkMissedExecutions(schedules, cfg, state) != nil {
		return false
	}
	return true
}

// getActualAlertReason determines the specific reason for an alert by checking which condition is triggered
func (a *Analyzer) getActualAlertReason(schedules []*database.CronSchedule, cfg config.DetectionConfig, state *JobState) string {
	// Check each condition and return the specific reason
	if alert := a.checkLongRunning(schedules, cfg, state); alert != nil {
		return alert.Reason
	}
	if alert := a.checkPendingAccumulation(schedules, cfg, state); alert != nil {
		return alert.Reason
	}
	if alert := a.checkConsecutiveErrors(schedules, cfg, state); alert != nil {
		return alert.Reason
	}
	if alert := a.checkMissedExecutions(schedules, cfg, state); alert != nil {
		return alert.Reason
	}
	
	// Fallback if no specific condition is met
	return "Multiple issues detected requiring attention"
}
