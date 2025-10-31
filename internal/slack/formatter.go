package slack

import (
	"fmt"
	"time"
)

// FormatAlert formats a cron alert for Slack
func FormatAlert(alert CronAlert) Message {
	if alert.Type == AlertTypeRecovered {
		return formatRecoveryMessage(alert)
	}
	return formatStuckMessage(alert)
}

// formatStuckMessage creates a Slack message for a stuck cron job
func formatStuckMessage(alert CronAlert) Message {
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
		Text: fmt.Sprintf("🚨 Cron job `%s` is stuck!", alert.CronCode),
		Blocks: []Block{
			{
				Type: "header",
				Text: &TextObject{
					Type: "plain_text",
					Text: "🚨 Stuck Cron Job Alert",
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Job:*\n`%s`", alert.CronCode)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Database Status:*\n%s", alert.Status)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Group:*\n%s", alert.CronGroup)},
					{Type: "mrkdwn", Text: "*Monitor Status:*\n🔴 Stuck"},
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
					{Type: "mrkdwn", Text: fmt.Sprintf("*Consecutive Issues:*\n%d", alert.ConsecutiveStuck)},
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

// formatRecoveryMessage creates a Slack message for a recovered cron job
func formatRecoveryMessage(alert CronAlert) Message {
	timestamp := alert.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")
	duration := formatDuration(alert.StuckDuration)
	
	lastExec := "Never"
	if !alert.LastExecution.IsZero() {
		lastExec = alert.LastExecution.UTC().Format("2006-01-02 15:04:05 UTC")
	}

	return Message{
		Text: fmt.Sprintf("✅ Cron job `%s` has recovered!", alert.CronCode),
		Blocks: []Block{
			{
				Type: "header",
				Text: &TextObject{
					Type: "plain_text",
					Text: "✅ Cron Job Recovered",
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Job:*\n`%s`", alert.CronCode)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Database Status:*\n%s", alert.Status)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Cron Group:*\n%s", alert.CronGroup)},
					{Type: "mrkdwn", Text: "*Monitor Status:*\n🟢 Healthy"},
				},
			},
			{
				Type: "section",
				Fields: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("*Was Stuck For:*\n%s ⏱️", duration)},
					{Type: "mrkdwn", Text: fmt.Sprintf("*Last Successful Execution:*\n%s", lastExec)},
				},
			},
			{
				Type: "section",
				Text: &TextObject{
					Type: "mrkdwn",
					Text: fmt.Sprintf("*📝 Recovery Details:*\n• Original Issue: %s", alert.Reason),
				},
			},
			{
				Type: "context",
				Elements: []TextObject{
					{Type: "mrkdwn", Text: fmt.Sprintf("🕒 Recovered at %s", timestamp)},
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
