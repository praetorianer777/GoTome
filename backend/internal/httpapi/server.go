// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
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
	r := chi.NewRouter()
	r.Use(requestID, logging(s.Log), recovery)
	r.NotFound(notFound)
	r.MethodNotAllowed(methodNotAllowed)

	r.Get(HealthPath, s.health)
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
