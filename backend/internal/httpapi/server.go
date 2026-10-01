// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
	"github.com/praetorianer777/gotome/backend/internal/settings"
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
	// Auth signs people in; Logins slows down guessing at passwords.
	Auth      *auth.Service
	Logins    *LoginLimits
	Libraries *library.Service
	// Settings are what an administrator changes while GOtome runs.
	Settings *settings.Store
	// Scans looks through library folders for new and changed files.
	Scans *ingest.Service
	// Books is the catalogue; Covers holds the cover images it points at.
	Books  *catalog.Service
	Covers *covers.Store
	// Metadata asks outside sources about books; Matches keeps what looking
	// books up by themselves found.
	Metadata *metadata.Service
	Matches  *enrich.Service
	// Web serves the web app for every path that is not the API's. Nil
	// answers those paths as not found, which is what the API tests want.
	Web http.Handler
}

// Routes is the handler for every path the server answers.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, logging(s.Log), recovery)
	r.NotFound(s.notFound)
	r.MethodNotAllowed(methodNotAllowed)

	r.Get(HealthPath, s.health)
	r.Get(ReadyPath, s.ready)
	r.Route(APIPrefix, func(api chi.Router) {
		api.Use(sameOrigin, s.authenticate)
		mount(api, s.routes())
	})
	return r
}

// mount puts the routes of a table on the router, each behind its permission.
func mount(api chi.Router, routes []Route) {
	for _, rt := range routes {
		// A route without a declared permission is a mistake in the table,
		// and must not be found out by whoever calls it first.
		if !auth.Known(rt.Permission) {
			panic(fmt.Sprintf("httpapi: route %s %s declares no known permission (%q)", rt.Method, rt.Path, rt.Permission))
		}
		api.Method(rt.Method, rt.Path, handle(require(rt.Permission, rt.Handler)))
	}
}

// handle adapts a HandlerFunc, turning the error it returns into the envelope.
func handle(h HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeError(w, r, err)
		}
	})
}

// notFound answers what no route matched. Under /api that is an error in the
// envelope; anywhere else it is an address of the web app, which routes in the
// browser.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if s.Web == nil || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, r, ErrNotFound(""))
		return
	}
	s.Web.ServeHTTP(w, r)
}

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
