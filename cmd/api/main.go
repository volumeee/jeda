package main

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/hibiken/asynq"

	"jeda/internal/config"
	"jeda/internal/handler"
	"jeda/internal/middleware"
	"jeda/pkg/logger"
	"jeda/ui"
)

func main() {
	cfg := config.LoadConfig()
	logger.SetupRedisLogger(cfg)

	client := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.RedisURL})
	defer client.Close()

	r := chi.NewRouter()
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Jeda-Forward-*", "Jeda-Deduplication-Id", "Jeda-Failure-Callback", "Jeda-Queue-Group", "Jeda-Env"},
		AllowCredentials: true,
	}))
	r.Use(middleware.RateLimit)

	// Embedded dashboard UI
	r.Handle("/ui/*", http.StripPrefix("/ui/", ui.Handler()))
	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("⚡ Jeda is running — visit /ui for the dashboard"))
	})

	manageH  := handler.NewManageHandler(cfg)
	taskH    := handler.NewTaskHandler(client, manageH.RDB(), cfg)

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.Auth(cfg))

		// Publish
		// Tasks (Unified)
		r.Post("/tasks",   taskH.Create)
		r.Post("/publish", taskH.Create) // Legacy fallback

		// Task management (REST)
		r.Get("/tasks",             manageH.ListTasks)
		r.Delete("/tasks/{id}",     manageH.DeleteTask)
		r.Post("/tasks/{id}/force",  manageH.ForceRun)
		r.Post("/tasks/{id}/exec",   manageH.ExecuteNow)
		r.Post("/tasks/{id}/update", manageH.UpdateTask)

		// Queue control
		r.Post("/queue/pause",  manageH.PauseQueue)
		r.Post("/queue/resume", manageH.ResumeQueue)

		// Logs (REST + SSE stream)
		r.Get("/logs",         manageH.GetLogs)
		r.Get("/logs/stream",  manageH.LogsStream)

		// Tasks SSE stream (replaces client-side polling)
		r.Get("/tasks/stream", manageH.TasksStream)

		// Fire test
		r.Post("/test-webhook", manageH.FireTestWebhook)
	})

	slog.Info("Starting Jeda API Server", "port", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, r); err != nil {
		slog.Error("Server failed", "err", err)
	}
}
