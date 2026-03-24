package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
	"os"

	"github.com/redis/go-redis/v9"
	"jeda/internal/config"
)

type RedisHandler struct {
	client *redis.Client
	next   slog.Handler
}

// SetupRedisLogger initializes globally the structured logger saving to Redis list
func SetupRedisLogger(cfg *config.Config) {
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisURL})

	h := &RedisHandler{
		client: rdb,
		// next:   slog.Default().Handler(),
		next: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}),
	}
	slog.SetDefault(slog.New(h))
}

func (h *RedisHandler) Handle(ctx context.Context, r slog.Record) error {
	// Let standard output print it first
	h.next.Handle(ctx, r)

	msg := map[string]interface{}{
		"time":  r.Time.Format(time.RFC3339),
		"level": r.Level.String(),
		"msg":   r.Message,
	}

	r.Attrs(func(a slog.Attr) bool {
		msg[a.Key] = a.Value.Any()
		return true
	})

	b, _ := json.Marshal(msg)
	h.client.LPush(ctx, "jeda:logs", string(b))
	h.client.LTrim(ctx, "jeda:logs", 0, 199) // Keep only the latest 200 logs

	return nil
}

func (h *RedisHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *RedisHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RedisHandler{client: h.client, next: h.next.WithAttrs(attrs)}
}

func (h *RedisHandler) WithGroup(name string) slog.Handler {
	return &RedisHandler{client: h.client, next: h.next.WithGroup(name)}
}
