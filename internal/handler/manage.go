package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"jeda/internal/config"
	"jeda/internal/models"
	"jeda/pkg/broadcast"
	"jeda/pkg/signature"
)

// ManageHandler holds all management endpoints and SSE broadcasters.
type ManageHandler struct {
	inspector *asynq.Inspector
	cfg       *config.Config
	rdb       *redis.Client
	logs      *broadcast.SSE
	tasks     *broadcast.SSE
}

// NewManageHandler creates the handler and starts background SSE feed goroutines.
func NewManageHandler(cfg *config.Config) *ManageHandler {
	h := &ManageHandler{
		inspector: asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.RedisURL}),
		cfg:       cfg,
		rdb:       redis.NewClient(&redis.Options{Addr: cfg.RedisURL}),
		logs:      broadcast.New(),
		tasks:     broadcast.New(),
	}
	go h.feedLogs()
	go h.feedTasks()
	return h
}

func (h *ManageHandler) RDB() *redis.Client { return h.rdb }

// ─────────────────────────────────────────────────
// Background feed goroutines
// ─────────────────────────────────────────────────

func (h *ManageHandler) feedLogs() {
	ctx := context.Background()
	pubsub := h.rdb.Subscribe(ctx, "jeda:logs:pubsub")
	defer pubsub.Close()
	
	ch := pubsub.Channel()
	for msg := range ch {
		h.logs.Send(msg.Payload)
	}
}

// feedTasks polls asynq inspector every 2s and broadcasts the full task payload when changed.
func (h *ManageHandler) feedTasks() {
	var lastJSON string
	for {
		time.Sleep(2 * time.Second)
		data := h.buildTasksPayload()
		msg, err := json.Marshal(data)
		if err != nil {
			continue
		}
		s := string(msg)
		if s == lastJSON {
			continue // no change, skip broadcast
		}
		lastJSON = s
		h.tasks.Send(s)
	}
}

// ─────────────────────────────────────────────────
// SSE stream endpoints
// ─────────────────────────────────────────────────

// LogsStream serves live log events via SSE.
func (h *ManageHandler) LogsStream(w http.ResponseWriter, r *http.Request) {
	h.logs.ServeHTTP(w, r, func() []string {
		items, _ := h.rdb.LRange(r.Context(), "jeda:logs", 0, 199).Result()
		out := make([]string, 0, len(items))
		for i := len(items) - 1; i >= 0; i-- {
			out = append(out, items[i])
		}
		return out
	})
}

// TasksStream serves live task+stats events via SSE.
func (h *ManageHandler) TasksStream(w http.ResponseWriter, r *http.Request) {
	h.tasks.ServeHTTP(w, r, func() []string {
		data := h.buildTasksPayload()
		msg, err := json.Marshal(data)
		if err != nil {
			return nil
		}
		return []string{string(msg)}
	})
}

// ─────────────────────────────────────────────────
// REST endpoints
// ─────────────────────────────────────────────────

// ListTasks returns the current queue stats and all pending/scheduled tasks (REST fallback).
func (h *ManageHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.buildTasksPayload())
}

// GetLogs returns the last 100 log entries as a JSON array (REST fallback).
func (h *ManageHandler) GetLogs(w http.ResponseWriter, r *http.Request) {
	items, err := h.rdb.LRange(r.Context(), "jeda:logs", 0, 99).Result()
	if err != nil {
		http.Error(w, "Failed to fetch logs", http.StatusInternalServerError)
		return
	}
	result := make([]interface{}, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		var p map[string]interface{}
		if json.Unmarshal([]byte(items[i]), &p) == nil {
			result = append(result, p)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// DeleteTask removes a task from the queue or deletes a cron schedule.
func (h *ManageHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id, queue := chi.URLParam(r, "id"), queueParam(r)

	if queue == "scheduler" {
		if err := h.rdb.HDel(r.Context(), "jeda:schedules", id).Err(); err != nil {
			http.Error(w, "Failed to delete cron task: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := h.inspector.DeleteTask(queue, id); err != nil {
		slog.Warn("DeleteTask failed", "err", err, "id", id)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ForceRun moves a scheduled/retry task to pending, or fires it directly if already pending.
func (h *ManageHandler) ForceRun(w http.ResponseWriter, r *http.Request) {
	id, queue := chi.URLParam(r, "id"), queueParam(r)
	w.Header().Set("Content-Type", "application/json")

	if queue == "scheduler" {
		data, err := h.rdb.HGet(r.Context(), "jeda:schedules", id).Result()
		if err != nil {
			http.Error(w, "Cron schedule not found", http.StatusNotFound)
			return
		}
		var entry struct {
			Payload models.WebhookPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			http.Error(w, "Invalid cron payload: "+err.Error(), http.StatusInternalServerError)
			return
		}
		
		// Fire it immediately
		bodyBytes, _ := json.Marshal(entry.Payload.Body)
		result := h.doHTTP(entry.Payload.Destination, bodyBytes, entry.Payload.Headers)
		
		httpStatus, _ := result["http_status"].(int)
		latency, _ := result["latency_ms"].(int64)
		if errMsg, ok := result["error"].(string); ok && errMsg != "" {
			slog.Warn("ExecuteNow error", "destination", entry.Payload.Destination, "err", errMsg, "latency_ms", latency)
		} else {
			slog.Info("ExecuteNow fired", "destination", entry.Payload.Destination, "status", httpStatus, "latency_ms", latency)
		}
		result["status"] = "fired"
		json.NewEncoder(w).Encode(result)
		return
	}

	err := h.inspector.RunTask(queue, id)
	if err == nil || strings.Contains(err.Error(), "already in pending state") {
		// Task is pending — fire it directly right now
		h.fireAndRespond(w, r, queue, id, true)
		return
	}
	// Task is active
	if strings.Contains(err.Error(), "task is in active state") {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Task is already actively running"})
		return
	}
	slog.Warn("ForceRun failed", "err", err, "id", id)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// ExecuteNow fires a task's webhook immediately and returns the HTTP response.
func (h *ManageHandler) ExecuteNow(w http.ResponseWriter, r *http.Request) {
	id, queue := chi.URLParam(r, "id"), queueParam(r)
	w.Header().Set("Content-Type", "application/json")
	h.fireAndRespond(w, r, queue, id, false)
}

// FireTestWebhook sends an instant test HTTP POST and returns the response.
func (h *ManageHandler) FireTestWebhook(w http.ResponseWriter, r *http.Request) {
	var req models.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	bodyBytes, _ := json.Marshal(req.Body)
	result := h.doHTTP(req.Destination, bodyBytes, nil)

	// Log the test result so it appears in Live Logs
	httpStatus, _ := result["http_status"].(int)
	latency, _ := result["latency_ms"].(int64)
	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		slog.Warn("FireTest failed", "destination", req.Destination, "err", errMsg, "latency_ms", latency)
	} else {
		slog.Info("FireTest executed", "destination", req.Destination, "status", httpStatus, "latency_ms", latency)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// UpdateTask updates an existing task's payload. Works for pending/scheduled tasks and cron schedules.
func (h *ManageHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id, queue := chi.URLParam(r, "id"), queueParam(r)

	var rawReq map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if queue == "scheduler" {
		data, err := h.rdb.HGet(r.Context(), "jeda:schedules", id).Result()
		if err != nil {
			http.Error(w, "Cron task not found", http.StatusNotFound)
			return
		}

		var entry struct {
			Cron     string                `json:"cron"`
			Timezone string                `json:"timezone"`
			Payload  models.WebhookPayload `json:"payload"`
		}
		json.Unmarshal([]byte(data), &entry)

		if v, ok := rawReq["destination"].(string); ok { entry.Payload.Destination = v }
		if v, ok := rawReq["env"].(string); ok { entry.Payload.Env = v }
		if v, ok := rawReq["body"]; ok { 
			bodyBytes, _ := json.Marshal(v)
			entry.Payload.Body = bodyBytes 
		}
		if v, ok := rawReq["failure_callback"].(string); ok { entry.Payload.FailureCallback = v }
		if v, ok := rawReq["cron"].(string); ok { entry.Cron = v }
		if v, ok := rawReq["timezone"].(string); ok { entry.Timezone = v }
		if v, ok := rawReq["retries"].(float64); ok { entry.Payload.MaxRetries = int(v) }
		if v, ok := rawReq["queue_group"].(string); ok { entry.Payload.FIFOQueueGroup = v }

		updatedBytes, _ := json.Marshal(entry)
		if err := h.rdb.HSet(r.Context(), "jeda:schedules", id, updatedBytes).Err(); err != nil {
			http.Error(w, "Failed to update cron: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Cron updated successfully"})
		return
	}

	// 1. Find existing task
	oldPayload, found := h.findTaskPayload(queue, id)
	if !found {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}

	// 2. Merge/Update payload
	if v, ok := rawReq["destination"].(string); ok { oldPayload.Destination = v }
	if v, ok := rawReq["env"].(string); ok { oldPayload.Env = v }
	if v, ok := rawReq["body"]; ok { 
		bodyBytes, _ := json.Marshal(v)
		oldPayload.Body = bodyBytes 
	}
	if v, ok := rawReq["failure_callback"].(string); ok { oldPayload.FailureCallback = v }
	if v, ok := rawReq["delay"].(string); ok { oldPayload.Delay = v }
	if v, ok := rawReq["retries"].(float64); ok { oldPayload.MaxRetries = int(v) }
	if v, ok := rawReq["dedup_id"].(string); ok { oldPayload.DeduplicationID = v }
	if v, ok := rawReq["queue_group"].(string); ok { oldPayload.FIFOQueueGroup = v }


	payloadBytes, _ := json.Marshal(oldPayload)

	// 3. Delete old task
	if err := h.inspector.DeleteTask(queue, id); err != nil {
		http.Error(w, "Delete failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Re-enqueue with new options
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: h.cfg.RedisURL})
	defer client.Close()

	taskOptions := []asynq.Option{asynq.TaskID(id)}
	queueName := "default"
	if oldPayload.FIFOQueueGroup != "" {
		queueName = "fifo-" + oldPayload.FIFOQueueGroup
	}
	taskOptions = append(taskOptions, asynq.Queue(queueName))

	if oldPayload.MaxRetries > 0 {
		taskOptions = append(taskOptions, asynq.MaxRetry(oldPayload.MaxRetries))
	}
	if oldPayload.Delay != "" {
		if duration, err := time.ParseDuration(oldPayload.Delay); err == nil {
			taskOptions = append(taskOptions, asynq.ProcessIn(duration))
		} else {
			layout := "2006-01-02 15:04:05" // Standard format
			if strings.Contains(oldPayload.Delay, "T") {
				layout = "2006-01-02T15:04" // HTML datetime-local format
				if strings.Count(oldPayload.Delay, ":") == 2 {
					layout = "2006-01-02T15:04:05"
				}
			}
			
			loc := time.UTC // default fallback
			if tz, ok := rawReq["timezone"].(string); ok && tz != "" {
				if parsedLoc, err := time.LoadLocation(tz); err == nil {
					loc = parsedLoc
				}
			}
			
			if parsedTime, err := time.ParseInLocation(layout, oldPayload.Delay, loc); err == nil {
				taskOptions = append(taskOptions, asynq.ProcessAt(parsedTime))
			} else {
				http.Error(w, "Invalid delay format", http.StatusBadRequest)
				return
			}
		}
	}
	taskOptions = append(taskOptions, asynq.Retention(24*time.Hour))

	task := asynq.NewTask("webhook:publish", payloadBytes, taskOptions...)
	if _, err := client.Enqueue(task); err != nil {
		http.Error(w, "Re-enqueue failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Task updated successfully"})
}

// PauseQueue pauses all queues.
func (h *ManageHandler) PauseQueue(w http.ResponseWriter, r *http.Request) {
	queues, _ := h.inspector.Queues()
	for _, q := range queues {
		h.inspector.PauseQueue(q)
	}
	w.WriteHeader(http.StatusOK)
}

// ResumeQueue resumes all queues.
func (h *ManageHandler) ResumeQueue(w http.ResponseWriter, r *http.Request) {
	queues, _ := h.inspector.Queues()
	for _, q := range queues {
		h.inspector.UnpauseQueue(q)
	}
	w.WriteHeader(http.StatusOK)
}

// ─────────────────────────────────────────────────
// Internal helpers
// ─────────────────────────────────────────────────

// buildTasksPayload gathers queue stats + all visible tasks from asynq inspector.
func (h *ManageHandler) buildTasksPayload() map[string]interface{} {
	queues, _ := h.inspector.Queues()
	stats := map[string]int{"pending": 0, "active": 0, "failed": 0, "success": 0}
	var all []map[string]interface{}

	for _, q := range queues {
		info, err := h.inspector.GetQueueInfo(q)
		if err != nil {
			continue
		}
		stats["pending"] += info.Pending + info.Scheduled
		stats["active"] += info.Active
		stats["failed"] += info.Retry + info.Archived
		stats["success"] += info.Completed

		if tasks, err := h.inspector.ListPendingTasks(q, asynq.PageSize(200)); err == nil {
			for _, t := range tasks {
				all = append(all, decodeTask(t.ID, t.Type, q, "pending", t.Payload))
			}
		}
		if tasks, err := h.inspector.ListScheduledTasks(q, asynq.PageSize(200)); err == nil {
			for _, t := range tasks {
				dt := decodeTask(t.ID, t.Type, q, "scheduled", t.Payload)
				dt["next_process_at"] = t.NextProcessAt
				all = append(all, dt)
			}
		}
	}

	// 4. Include Periodic (Cron) Tasks from Redis
	scheduleKey := "jeda:schedules"
	data, _ := h.rdb.HGetAll(context.Background(), scheduleKey).Result()
	for id, jsonStr := range data {
		var entry struct {
			Cron     string                `json:"cron"`
			Timezone string                `json:"timezone"`
			Payload  models.WebhookPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &entry); err == nil {
			all = append(all, map[string]interface{}{
				"id":               id,
				"type":             "webhook:cron",
				"queue":            "scheduler",
				"state":            "periodic",
				"env":              entry.Payload.Env,
				"destination":      entry.Payload.Destination,
				"body":             decodeBody(entry.Payload.Body),
				"failure_callback": entry.Payload.FailureCallback,
				"cron":             entry.Cron,
				"timezone":         entry.Timezone,
				"retries":          entry.Payload.MaxRetries,
				"queue_group":      entry.Payload.FIFOQueueGroup,
			})
		}
	}

	if all == nil {
		all = []map[string]interface{}{}
	}
	return map[string]interface{}{"stats": stats, "tasks": all}
}

// decodeTask converts raw asynq task bytes to a JSON-friendly map.
// Supports both the new format (body = raw JSON) and the legacy format (body = base64-encoded JSON).
func decodeTask(id, taskType, queue, state string, rawPayload []byte) map[string]interface{} {
	var p models.WebhookPayload
	if err := json.Unmarshal(rawPayload, &p); err != nil {
		return map[string]interface{}{"id": id, "type": taskType, "queue": queue, "state": state, "env": "production"}
	}
	env := p.Env
	if env == "" {
		env = "production"
	}
	return map[string]interface{}{
		"id":               id,
		"type":             taskType,
		"queue":            queue,
		"state":            state,
		"env":              p.Env,
		"destination":      p.Destination,
		"body":             decodeBody(p.Body),
		"failure_callback": p.FailureCallback,
		"retries":          p.MaxRetries,
		"delay":            p.Delay,
		"dedup_id":         p.DeduplicationID,
		"queue_group":      p.FIFOQueueGroup,
	}
}

// decodeBody converts a json.RawMessage body to a displayable value.
// Handles the old format where body was base64-encoded.
func decodeBody(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	var parsed interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return string(raw)
	}
	// Detect legacy base64-encoded string
	if str, ok := parsed.(string); ok {
		if decoded, err := base64.StdEncoding.DecodeString(str); err == nil {
			var obj interface{}
			if json.Unmarshal(decoded, &obj) == nil {
				return obj
			}
			return string(decoded)
		}
		return str
	}
	return parsed
}

// fireAndRespond finds a task by ID, fires its webhook, and writes the HTTP response as JSON.
func (h *ManageHandler) fireAndRespond(w http.ResponseWriter, r *http.Request, queue, id string, alreadyPending bool) {
	payload, found := h.findTaskPayload(queue, id)
	if !found {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	bodyBytes, _ := json.Marshal(payload.Body)
	result := h.doHTTP(payload.Destination, bodyBytes, payload.Headers)
	result["status"] = "fired"
	result["already_pending"] = alreadyPending
	result["message"] = fmt.Sprintf("Fired to %s — HTTP %v in %vms", payload.Destination, result["http_status"], result["latency_ms"])

	httpStatus, _ := result["http_status"].(int)
	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		slog.Warn("ExecuteNow error", "destination", payload.Destination, "err", errMsg, "latency_ms", result["latency_ms"])
	} else {
		slog.Info("ExecuteNow fired", "destination", payload.Destination, "status", httpStatus, "latency_ms", result["latency_ms"])
	}

	// Remove from queue so it doesn't stay 'pending' — successful direct fire = done
	if delErr := h.inspector.DeleteTask(queue, id); delErr != nil {
		slog.Warn("fireAndRespond: could not delete task after firing", "id", id, "err", delErr)
	}

	json.NewEncoder(w).Encode(result)
}

// findTaskPayload searches pending and scheduled lists for a task with the given ID.
func (h *ManageHandler) findTaskPayload(queue, id string) (models.WebhookPayload, bool) {
	type taskEntry struct {
		id      string
		payload []byte
	}

	var entries []taskEntry
	if pending, err := h.inspector.ListPendingTasks(queue, asynq.PageSize(500)); err == nil {
		for _, t := range pending {
			entries = append(entries, taskEntry{t.ID, t.Payload})
		}
	}
	if scheduled, err := h.inspector.ListScheduledTasks(queue, asynq.PageSize(500)); err == nil {
		for _, t := range scheduled {
			entries = append(entries, taskEntry{t.ID, t.Payload})
		}
	}

	for _, e := range entries {
		if e.id == id {
			var p models.WebhookPayload
			if json.Unmarshal(e.payload, &p) == nil {
				return p, true
			}
		}
	}
	return models.WebhookPayload{}, false
}

// doHTTP performs a POST request and returns a response map.
func (h *ManageHandler) doHTTP(destination string, bodyBytes []byte, extraHeaders map[string]string) map[string]interface{} {
	req, err := http.NewRequest("POST", destination, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return map[string]interface{}{"error": err.Error(), "latency_ms": 0}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Jeda-Signature", signature.Generate(h.cfg.SigningKey, bodyBytes))
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return map[string]interface{}{"error": err.Error(), "latency_ms": latency}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	hdrs := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		hdrs[k] = strings.Join(v, ", ")
	}
	return map[string]interface{}{
		"http_status": resp.StatusCode,
		"latency_ms":  latency,
		"body":        string(respBody),
		"headers":     hdrs,
	}
}

func queueParam(r *http.Request) string {
	if q := r.URL.Query().Get("queue"); q != "" {
		return q
	}
	return "default"
}
