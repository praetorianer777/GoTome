// Package httpapi serves the HTTP API.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// HealthPath answers as soon as the process serves HTTP. The container health
// check and the healthcheck subcommand both probe it.
const HealthPath = "/healthz"

// Server holds what the handlers depend on.
type Server struct {
	Log *slog.Logger
}

// Routes is the handler for every path the server answers.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+HealthPath, s.health)
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.Log.Warn("health response not written", "error", err)
	}
}
