package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/hibiken/asynq"
	"jeda/internal/config"
	"jeda/internal/models"
	"jeda/pkg/signature"
)

type WebhookProcessor struct {
	cfg        *config.Config
	httpClient *http.Client
}

func NewWebhookProcessor(cfg *config.Config) *WebhookProcessor {
	return &WebhookProcessor{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (p *WebhookProcessor) ProcessTask(ctx context.Context, t *asynq.Task) error {
	var payload models.WebhookPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("json.Unmarshal failed: %v", err)
	}

	slog.Info("Mengirim webhook", "destination", payload.Destination, "task_id", t.ResultWriter().TaskID())

	req, err := http.NewRequestWithContext(ctx, "POST", payload.Destination, bytes.NewBuffer(payload.Body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range payload.Headers {
		req.Header.Set(k, v)
	}

	sig := signature.Generate(p.cfg.SigningKey, payload.Body)
	req.Header.Set("Jeda-Signature", sig)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		slog.Error("Webhook HTTP request failed (Timeout/Down)", "dest", payload.Destination, "err", err)
		return err // Return err to trigger Asynq Retry (with Exponential Backoff)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Warn("Webhook returned error status", "dest", payload.Destination, "status", resp.StatusCode)
		return fmt.Errorf("webhook returned non-200 status: %d", resp.StatusCode)
	}

	slog.Info("✅ Webhook sukses terkirim", "dest", payload.Destination, "status", resp.StatusCode)
	return nil
}
