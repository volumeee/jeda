package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"jeda/internal/config"
	"jeda/internal/models"
)

type TaskHandler struct {
	client   *asynq.Client
	rdb      *redis.Client
	cfg      *config.Config
	validate *validator.Validate
}

func NewTaskHandler(client *asynq.Client, rdb *redis.Client, cfg *config.Config) *TaskHandler {
	return &TaskHandler{
		client:   client,
		rdb:      rdb,
		cfg:      cfg,
		validate: validator.New(),
	}
}

// Create handles POST /v1/tasks. It can be a one-off (webhook) or periodic (cron).
func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.validate.Struct(req); err != nil {
		http.Error(w, "Validation failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 1. Extract Jeda Metadata
	failureCallback := req.FailureCallback
	dedupID := req.DedupID
	queueGroup := req.QueueGroup
	env := req.Env
	if env == "" {
		env = "production"
	}

	// Forward Headers (legacy support)
	jedaHeaders := make(map[string]string)
	for k, v := range r.Header {
		if strings.HasPrefix(strings.ToLower(k), "jeda-forward-") {
			if len(k) > 13 {
				jedaHeaders[k[13:]] = v[0]
			}
		}
	}

	body := req.Body
	if body == nil {
		body = json.RawMessage(`null`)
	}

	payload := models.WebhookPayload{
		TaskBase: models.TaskBase{
			Destination:     req.Destination,
			FailureCallback: failureCallback,
			Headers:         jedaHeaders,
			Body:            body,
			Env:             env,
		},
		MaxRetries:      req.Retries,
		Delay:           req.Delay,
		DeduplicationID: dedupID,
		FIFOQueueGroup:  queueGroup,
	}
	if payload.MaxRetries == 0 {
		payload.MaxRetries = 3
	}

	// 2. Decide: Cron or One-off?
	if req.Cron != "" {
		h.handleCron(w, r, req, payload)
		return
	}

	h.handleOneOff(w, r, req, payload)
}

func (h *TaskHandler) handleOneOff(w http.ResponseWriter, r *http.Request, req models.TaskRequest, payload models.WebhookPayload) {
	payloadBytes, _ := json.Marshal(payload)
	taskOptions := []asynq.Option{
		asynq.MaxRetry(payload.MaxRetries),
		asynq.Retention(24 * time.Hour),
	}

	if req.Delay != "" {
		if duration, err := time.ParseDuration(req.Delay); err == nil {
			// It's a relative duration like "10s", "5m"
			taskOptions = append(taskOptions, asynq.ProcessIn(duration))
		} else {
			// Try parsing as absolute datetime
			layout := "2006-01-02 15:04:05" // Standard format
			if strings.Contains(req.Delay, "T") {
				layout = "2006-01-02T15:04" // HTML datetime-local format
				if strings.Count(req.Delay, ":") == 2 {
					layout = "2006-01-02T15:04:05"
				}
			}
			
			loc, err := time.LoadLocation(req.Timezone)
			if err != nil {
				loc = time.UTC // fallback
			}
			
			if parsedTime, err := time.ParseInLocation(layout, req.Delay, loc); err == nil {
				taskOptions = append(taskOptions, asynq.ProcessAt(parsedTime))
			} else {
				http.Error(w, "Invalid delay format. Use duration (e.g., 10s) or datetime (YYYY-MM-DD HH:mm:ss)", http.StatusBadRequest)
				return
			}
		}
	}

	if payload.DeduplicationID != "" {
		taskOptions = append(taskOptions, asynq.TaskID(payload.DeduplicationID))
	}

	queueName := "default"
	if payload.FIFOQueueGroup != "" {
		queueName = "fifo-" + payload.FIFOQueueGroup
	}
	taskOptions = append(taskOptions, asynq.Queue(queueName))

	task := asynq.NewTask("webhook:publish", payloadBytes, taskOptions...)
	info, err := h.client.Enqueue(task)
	if err != nil {
		if strings.Contains(err.Error(), "task ID conflicts") {
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"status": "duplicate_ignored", "id": payload.DeduplicationID})
			return
		}
		http.Error(w, "Enqueue failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": info.ID, "status": "enqueued"})
}

func (h *TaskHandler) handleCron(w http.ResponseWriter, r *http.Request, req models.TaskRequest, payload models.WebhookPayload) {
	// For dynamic cron, we store the schedule in Redis.
	// The Worker process will run an asynq.Scheduler that periodically reloads these.
	
	scheduleKey := "jeda:schedules"
	id := payload.DeduplicationID
	if id == "" {
		id = uuid.New().String()
	}

	scheduleData := map[string]interface{}{
		"id":       id,
		"cron":     req.Cron,
		"timezone": req.Timezone,
		"payload":  payload,
		"updated":  time.Now(),
	}
	jsonBytes, _ := json.Marshal(scheduleData)

	if err := h.rdb.HSet(r.Context(), scheduleKey, id, jsonBytes).Err(); err != nil {
		http.Error(w, "Failed to save schedule: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": "scheduled", "cron": req.Cron})
}
