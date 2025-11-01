package slack

import (
	"fmt"
	"time"
)

// FormatAlert formats a CronAlert into a Slack message
func FormatAlert(alert CronAlert) Message {
	if alert.Type == AlertTypeAlerting {
		return formatAlertingMessage(alert)
	}
	return formatNotAlertingMessage(alert)
}

// formatAlertingMessage creates a detailed alerting cron alert message
func formatAlertingMessage(alert CronAlert) Message {
	timestamp := alert.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
	lastExec := "Never"
	if !alert.LastExecution.IsZero() {
		lastExec = alert.LastExecution.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	
	scheduledAt := "N/A"
	if alert.ScheduledAt != nil && !alert.ScheduledAt.IsZero() {
		scheduledAt = alert.ScheduledAt.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	
	runningTime := "N/A"
	if alert.RunningTime != nil {
		runningTime = formatDuration(*alert.RunningTime)
	}

	return Message{
		Text: fmt.Sprintf("🚨 Cron job `%s` is alerting!", alert.CronCode),
		Blocks: []Block{
			{
				Type: "header",
				Text: &TextObject{
					Type: "plain_text",
					Text: "🚨 Cron Job Alert",
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Job:*\n`%s`", alert.CronCode)},
					{Type: "mrkdwn", Text: "*Monitor Status:*\n🔴 Alerting"},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Consecutive Issues:*\n%d", alert.ConsecutiveStuck)},
				},
			},
			{
				Type: "section",
				Text: &TextObject{
					Type: "mrkdwn",
					Text: "*⏱️ Timing Details:*",
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Scheduled At:*\n%s", scheduledAt)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Last Execution:*\n%s", lastExec)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Running Time:*\n%s", runningTime)},
				},
			},
			{
				Type: "section",
				Text: &TextObject{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*🔍 Problem Details:*\n%s", alert.Reason),
				},
			},
			{
				Type: "context",
				Elements: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("🕒 Alerted at %s", timestamp)},
				},
			},
		},
	}
}

// formatNotAlertingMessage creates a Slack message for a cron job that's no longer alerting
func formatNotAlertingMessage(alert CronAlert) Message {
	timestamp := alert.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
	duration := formatDuration(alert.StuckDuration)
	
	lastExec := "Never"
	if !alert.LastExecution.IsZero() {
		lastExec = alert.LastExecution.UTC().Format("2006-01-02 15:04:05 UTC")
	}

	return Message{
		Text: fmt.Sprintf("✅ Cron job `%s` is no longer alerting!", alert.CronCode),
		Blocks: []Block{
			{
				Type: "header",
				Text: &TextObject{
					Type: "plain_text",
					Text: "✅ Cron Job No Longer Alerting",
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Job:*\n`%s`", alert.CronCode)},
					{Type: "mrkdwn", Text: "*Monitor Status:*\n🟢 Not Alerting"},
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Was Alerting For:*\n%s ⏱️", duration)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Last Successful Execution:*\n%s", lastExec)},
				},
			},
			{
				Type: "section",
				Text: &TextObject{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*📝 Resolution Details:*\n• Original Issue: %s", alert.Reason),
				},
			},
			{
				Type: "context",
				Elements: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("🕒 No longer alerting at %s", timestamp)},
				},
			},
		},
	}
}

// formatDuration formats a duration in human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%d minutes", minutes)
		}
		return fmt.Sprintf("%d minutes %d seconds", minutes, seconds)
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%d hours", hours)
	}
	return fmt.Sprintf("%d hours %d minutes", hours, minutes)
}
