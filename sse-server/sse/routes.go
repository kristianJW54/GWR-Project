package sse

import (
	"log/slog"
	"net/http"
)

func addRoutes(mux *http.ServeMux, logger *slog.Logger, config *Config, es *EventServer) {
	adminMiddleware := AdminAPIAccessMiddleware(config, logger)
	mux.Handle("/admin/events/all", adminMiddleware(http.HandlerFunc(es.HandleConnection)))
}

func AdminAPIAccessMiddleware(config *Config, logger *slog.Logger) func(h http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			// Check if the token matches the expected admin token
			if token != config.AdminToken {
				// Log the unauthorized access attempt with the remote address
				logger.Warn("Unauthorized access attempt", "remote_addr", r.RemoteAddr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			h.ServeHTTP(w, r)
		})
	}
}
