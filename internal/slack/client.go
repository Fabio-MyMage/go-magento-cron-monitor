package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Config represents Slack notification configuration
type Config struct {
	Enabled          bool          `yaml:"enabled"`
	WebhookURLs      []string      `yaml:"webhook_urls"`
	AlertCooldown    time.Duration `yaml:"alert_cooldown"`
	SendRecovery     bool          `yaml:"send_recovery"`
	RecoveryCooldown time.Duration `yaml:"recovery_cooldown"`
	Timeout          time.Duration `yaml:"timeout"`
}

// Client handles Slack webhook notifications
type Client struct {
	config     Config
	httpClient *http.Client
}

// New creates a new Slack client
func New(config Config) *Client {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// SendAlert sends a cron alert to all configured Slack webhooks
func (c *Client) SendAlert(alert CronAlert) error {
	if !c.config.Enabled {
		return nil
	}

	if len(c.config.WebhookURLs) == 0 {
		return fmt.Errorf("no slack webhook URLs configured")
	}

	// Format the message once
	message := FormatAlert(alert)

	// Marshal to JSON once
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	// Send to all webhooks
	var lastError error
	successCount := 0

	for i, webhookURL := range c.config.WebhookURLs {
		if webhookURL == "" {
			continue
		}

		// Send to Slack
		resp, err := c.httpClient.Post(
			webhookURL,
			"application/json",
			bytes.NewBuffer(payload),
		)
		if err != nil {
			lastError = fmt.Errorf("webhook %d failed: %w", i+1, err)
			continue
		}

		// Check response
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastError = fmt.Errorf("webhook %d returned non-OK status: %d", i+1, resp.StatusCode)
			continue
		}

		resp.Body.Close()
		successCount++
	}

	// If all webhooks failed, return the last error
	if successCount == 0 && lastError != nil {
		return lastError
	}

	return nil
}

// GetConfig returns the client configuration
func (c *Client) GetConfig() Config {
	return c.config
}
