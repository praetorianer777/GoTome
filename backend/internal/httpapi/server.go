// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// HealthPath answers as soon as the process serves HTTP: the process is alive.
const HealthPath = "/healthz"

// ReadyPath answers 200 only while the database does. The container health
// check and the healthcheck subcommand probe it.
const ReadyPath = "/readyz"

// readinessTimeout bounds the database round trip behind ReadyPath, so a probe
// gets an answer before its own deadline passes.
const readinessTimeout = 3 * time.Second

// Database is what the server asks of the database directly; *pgxpool.Pool
// is the real one.
type Database interface {
	Ping(ctx context.Context) error
}

// Server holds what the handlers depend on.
type Server struct {
	Log *slog.Logger
	DB  Database
}

// Routes is the handler for every path the server answers.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, logging(s.Log), recovery)
	r.NotFound(notFound)
	r.MethodNotAllowed(methodNotAllowed)

	r.Get(HealthPath, s.health)
	r.Get(ReadyPath, s.ready)
	r.Route(APIPrefix, func(api chi.Router) {
		for _, rt := range s.routes() {
			api.Method(rt.Method, rt.Path, handle(rt.Handler))
		}
	})
	return r
}

// handle adapts a HandlerFunc, turning the error it returns into the envelope.
func handle(h HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeError(w, r, err)
		}
	})
}

func notFound(w http.ResponseWriter, r *http.Request) { writeError(w, r, ErrNotFound("")) }

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, ErrMethodNotAllowed())
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		loggerFrom(r.Context()).Warn("not ready: the database does not answer", "error", err)
		writeError(w, r, ErrUnavailable("The database does not answer."))
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}
