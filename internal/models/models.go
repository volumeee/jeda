package models

import "encoding/json"

// TaskBase contains common fields for Webhook tasks
type TaskBase struct {
	Destination     string            `json:"destination" validate:"required,url"`
	Body            json.RawMessage   `json:"body"`
	Headers         map[string]string `json:"headers"`
	FailureCallback string            `json:"failure_callback,omitempty" validate:"omitempty,url"`
	Env             string            `json:"env,omitempty"` // "production" | "staging"
}

// WebhookPayload is the data saved in Redis for the worker to execute
type WebhookPayload struct {
	TaskBase
	// Metadata for dashboard display
	MaxRetries      int    `json:"max_retries,omitempty"`
	Delay           string `json:"delay,omitempty"`
	DeduplicationID string `json:"dedup_id,omitempty"`
	FIFOQueueGroup  string `json:"queue_group,omitempty"`
}

// TaskRequest is the unified request for creating any task (immediate, delayed, or cron)
type TaskRequest struct {
	TaskBase
	// For one-off tasks
	Delay   string `json:"delay" validate:"omitempty,ascii"`
	Retries    int    `json:"retries" validate:"omitempty,min=0,max=100"`
	DedupID    string `json:"dedup_id,omitempty" validate:"omitempty,ascii"`
	QueueGroup string `json:"queue_group,omitempty" validate:"omitempty,ascii"`

	// For periodic (cron) tasks
	Cron     string `json:"cron" validate:"omitempty"`
	Timezone string `json:"timezone" validate:"omitempty,ascii"`
}
