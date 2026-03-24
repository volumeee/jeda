package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"jeda/internal/config"
	"jeda/internal/models"
	"jeda/internal/scheduler"
	"jeda/internal/worker"
	"jeda/pkg/logger"
)

func main() {
	cfg := config.LoadConfig()
	logger.SetupRedisLogger(cfg)

	redisOpt := asynq.RedisClientOpt{Addr: cfg.RedisURL}

	srv := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: 10,
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			ShutdownTimeout: 15 * time.Second,
			Logger:          &logger.AsynqLogger{},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				slog.Error("💀 Task masuk DLQ (Max Retries habis)", "type", task.Type(), "err", err)

				// Fire failure callback if configured
				var payload models.WebhookPayload
				if parseErr := json.Unmarshal(task.Payload(), &payload); parseErr == nil {
					if payload.FailureCallback != "" {
						slog.Info("🔔 Firing Failure Callback", "url", payload.FailureCallback)
						callbackBody, _ := json.Marshal(map[string]interface{}{
							"task_id":              task.ResultWriter().TaskID(),
							"status":               "failed",
							"reason":               err.Error(),
							"original_destination": payload.Destination,
						})
						http.Post(payload.FailureCallback, "application/json", bytes.NewBuffer(callbackBody))
					}
				}
			}),
		},
	)

	processor := worker.NewWebhookProcessor(cfg)
	mux := asynq.NewServeMux()
	mux.HandleFunc("webhook:publish", processor.ProcessTask)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		slog.Info("🔧 Starting Jeda Worker...")
		if err := srv.Start(mux); err != nil {
			slog.Error("Failed starting worker", "error", err)
			os.Exit(1)
		}
	}()

	// Jeda Dynamic Scheduler
	sch := scheduler.New(cfg.RedisURL)
	go func() {
		if err := sch.Start(context.Background()); err != nil {
			slog.Error("Failed starting scheduler", "error", err)
		}
	}()

	<-sigs
	slog.Info("Sinyal SIGTERM diterima. Graceful Shutdown dimulai...")
	srv.Shutdown()
	// No easy way to shutdown sch.Run() unless we implement ctx. But Run() is blocking.
	// We'll rely on process exit.
	slog.Info("✅ Jeda Worker berhenti dengan selamat.")
}
