package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"jeda/internal/models"
	"jeda/pkg/logger"
)

type DynamicScheduler struct {
	scheduler *asynq.Scheduler
	rdb       *redis.Client
	entries   map[string]string // id -> cron string to track changes
}

func New(redisURL string) *DynamicScheduler {
	opts := asynq.RedisClientOpt{Addr: redisURL}
	
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		redisOpts = &redis.Options{Addr: redisURL}
	}
	return &DynamicScheduler{
		scheduler: asynq.NewScheduler(opts, &asynq.SchedulerOpts{
			Logger: &logger.AsynqLogger{},
		}),
		rdb:       redis.NewClient(redisOpts),
		entries:   make(map[string]string),
	}
}

func (s *DynamicScheduler) Start(ctx context.Context) error {
	slog.Info("⏰ Starting Dynamic Jeda Scheduler...")

	// Initial load
	s.sync(ctx)

	// Periodic sync every 30 seconds
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sync(ctx)
			}
		}
	}()

	return s.scheduler.Run()
}

func (s *DynamicScheduler) sync(ctx context.Context) {
	scheduleKey := "jeda:schedules"
	data, err := s.rdb.HGetAll(ctx, scheduleKey).Result()
	if err != nil {
		slog.Error("Failed to fetch schedules from Redis", "err", err)
		return
	}

	for id, jsonStr := range data {
		var entry struct {
			Cron     string                `json:"cron"`
			Timezone string                `json:"timezone"`
			Payload  models.WebhookPayload `json:"payload"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &entry); err != nil {
			continue
		}

		// If new or changed, register
		if s.entries[id] != entry.Cron {
			payloadBytes, _ := json.Marshal(entry.Payload)
			task := asynq.NewTask("webhook:publish", payloadBytes)

			// Combine Timezone with Cron expression
			cronSpec := entry.Cron
			if entry.Timezone != "" {
				cronSpec = "CRON_TZ=" + entry.Timezone + " " + cronSpec
			}
			
			// Build enqueue options
			queueName := "default"
			if entry.Payload.FIFOQueueGroup != "" {
				queueName = "fifo-" + entry.Payload.FIFOQueueGroup
			}
			opts := []asynq.Option{
				asynq.TaskID(id),
				asynq.Queue(queueName),
			}
			if entry.Payload.MaxRetries > 0 {
				opts = append(opts, asynq.MaxRetry(entry.Payload.MaxRetries))
			}

			_, err := s.scheduler.Register(cronSpec, task, opts...)
			if err != nil {
				slog.Error("Failed to register cron task", "id", id, "cron", cronSpec, "err", err)
			} else {
				slog.Info("✅ Registered Cron Task", "id", id, "cron", cronSpec)
				s.entries[id] = entry.Cron
			}
		}
	}
}
