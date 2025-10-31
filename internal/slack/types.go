package slack

import "time"

// AlertType represents the type of alert
type AlertType string

const (
	AlertTypeStuck     AlertType = "stuck"
	AlertTypeRecovered AlertType = "recovered"
)

// CronAlert represents a cron job alert for Slack
type CronAlert struct {
	Type          AlertType
	CronCode      string        // e.g., "indexer_reindex_all_invalid"
	Status        string        // e.g., "pending", "running", "missed"
	LastExecution time.Time
	StuckDuration time.Duration // For recovery notifications
	Timestamp     time.Time
	
	// Enhanced fields for detailed alerts
	CronGroup        string
	RunningTime      *time.Duration
	ScheduledAt      *time.Time
	Reason           string
	ConsecutiveStuck int
	PendingCount     int
	ErrorCount       int
	MissedCount      int
}

// Message represents a Slack message with blocks
type Message struct {
	Text   string  `json:"text"`
	Blocks []Block `json:"blocks"`
}

// Block represents a Slack block
type Block struct {
	Type     string       `json:"type"`
	Text     *TextObject  `json:"text,omitempty"`
	Fields   []TextObject `json:"fields,omitempty"`
	Elements []TextObject `json:"elements,omitempty"`
}

// TextObject represents text in a Slack block
type TextObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
