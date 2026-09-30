package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// RequestIDHeader carries the ID back to the client on every response.
const RequestIDHeader = "X-Request-Id"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	loggerKey
)

// RequestIDFrom is the ID of the request a context belongs to, or "".
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// loggerFrom is the request's logger, which names the request in every line.
func loggerFrom(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}

// requestID names every request. The ID is always ours: one taken from the
// client could be made to collide with another request's lines in the log.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [12]byte
		_, _ = rand.Read(raw[:])
		id := hex.EncodeToString(raw[:])
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// statusRecorder remembers what a handler answered, for the log line.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer, for flushing and
// per-request deadlines.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logging writes one line per request and hands the handlers a logger that
// carries the request ID.
func logging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			log := base.With("requestId", RequestIDFrom(r.Context()))
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), loggerKey, log)))

			// The container health check asks every few seconds; at info it
			// would be most of the log.
			level := slog.LevelInfo
			if r.URL.Path == HealthPath {
				level = slog.LevelDebug
			}
			log.Log(r.Context(), level, "request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(started),
			)
		})
	}
}

// recovery turns a panic in a handler into the error envelope, so one bad
// request does not take the connection down without an answer.
func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			// The server uses this value to abort a response on purpose.
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			loggerFrom(r.Context()).Error("handler panicked", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			writeError(w, r, ErrInternal(fmt.Errorf("panic: %v", rec)))
		}()
		next.ServeHTTP(w, r)
	})
}
