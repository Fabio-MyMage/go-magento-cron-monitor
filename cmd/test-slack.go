package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/fabio/go-magento-cron-monitor/internal/slack"
)

// TestSlackData represents the JSON input for testing Slack alerts
type TestSlackData struct {
	ConsecutiveStuck int    `json:"consecutive_stuck"`
	CronGroup        string `json:"cron_group"`
	ExecutedAt       string `json:"executed_at"`
	JobCode          string `json:"job_code"`
	Reason           string `json:"reason"`
	RunningTime      string `json:"running_time"`
	ScheduledAt      string `json:"scheduled_at"`
	Status           string `json:"status"`
}

var testSlackCmd = &cobra.Command{
	Use:   "test-slack <webhook-url> <alert-json>",
	Short: "Test Slack notifications with custom alert data",
	Long: `Test Slack notifications by sending a sample alert or recovery message.

Examples:
  # Test alerting notification
  go-magento-cron-monitor test-slack "https://hooks.slack.com/..." '{"consecutive_stuck":6,"cron_group":"default","executed_at":"2025-10-31T09:21:21Z","job_code":"image_binder_run","reason":"job running longer than max_running_time threshold (1h0m0s)","running_time":"1h9m11.666374962s","scheduled_at":"2025-10-31T09:20:00Z","status":"running"}'

  # Test recovery notification
  go-magento-cron-monitor test-slack "https://hooks.slack.com/..." '{"consecutive_stuck":0,"cron_group":"default","executed_at":"2025-10-31T09:21:21Z","job_code":"image_binder_run","reason":"Issues resolved - cron job returned to normal operation","scheduled_at":"2025-10-31T09:20:00Z","status":"success"}' --recovery`,
	Args: cobra.ExactArgs(2),
	RunE: runTestSlack,
}

var recoveryFlag bool

func init() {
	rootCmd.AddCommand(testSlackCmd)
	testSlackCmd.Flags().BoolVar(&recoveryFlag, "recovery", false, "Send a recovery notification instead of alerting")
}

func runTestSlack(cmd *cobra.Command, args []string) error {
	webhookURL := args[0]
	alertJSON := args[1]

	// Parse the JSON input
	var testData TestSlackData
	if err := json.Unmarshal([]byte(alertJSON), &testData); err != nil {
		return fmt.Errorf("failed to parse alert JSON: %w", err)
	}

	// Parse timestamps
	var executedAt time.Time
	var scheduledAt time.Time
	var runningTime *time.Duration
	
	if testData.ExecutedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, testData.ExecutedAt); err == nil {
			executedAt = parsed
		}
	}
	
	if testData.ScheduledAt != "" {
		if parsed, err := time.Parse(time.RFC3339, testData.ScheduledAt); err == nil {
			scheduledAt = parsed
		}
	}
	
	if testData.RunningTime != "" {
		if parsed, err := time.ParseDuration(testData.RunningTime); err == nil {
			runningTime = &parsed
		}
	}

	// Create the alert
	alertType := slack.AlertTypeAlerting
	stuckDuration := time.Duration(0)
	
	if recoveryFlag {
		alertType = slack.AlertTypeNotAlerting
		// For recovery, calculate how long it was stuck (use running time as proxy)
		if runningTime != nil {
			stuckDuration = *runningTime
		} else {
			stuckDuration = 10 * time.Minute // Default for demo
		}
	}

	alert := slack.CronAlert{
		Type:             alertType,
		CronCode:         testData.JobCode,
		Status:           testData.Status,
		LastExecution:    executedAt,
		StuckDuration:    stuckDuration,
		Timestamp:        time.Now(),
		CronGroup:        testData.CronGroup,
		RunningTime:      runningTime,
		ScheduledAt:      &scheduledAt,
		Reason:           testData.Reason,
		ConsecutiveStuck: testData.ConsecutiveStuck,
	}

	// Create Slack client and send alert
	config := slack.Config{
		Enabled:     true,
		WebhookURLs: []string{webhookURL},
		Timeout:     10 * time.Second,
	}
	client := slack.New(config)
	
	fmt.Printf("Sending %s notification to Slack...\n", alertType)
	fmt.Printf("Webhook URL: %s\n", webhookURL)
	fmt.Printf("Alert data: %+v\n\n", alert)
	
	if err := client.SendAlert(alert); err != nil {
		return fmt.Errorf("failed to send Slack alert: %w", err)
	}

	fmt.Printf("✅ Successfully sent %s notification to Slack!\n", alertType)
	return nil
}