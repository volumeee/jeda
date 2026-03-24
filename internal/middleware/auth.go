package middleware

import (
	"net/http"
	"strings"

	"jeda/internal/config"
)

// Auth validates Bearer token from Authorization header OR ?key= query param (for SSE EventSource).
func Auth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := ""

			// 1. Try Authorization header first
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && parts[0] == "Bearer" {
					token = parts[1]
				}
			}

			// 2. Fallback: ?key= query param (for EventSource / SSE)
			if token == "" {
				token = r.URL.Query().Get("key")
			}

			if token == "" || token != cfg.APIKey {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

