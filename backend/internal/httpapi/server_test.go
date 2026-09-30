package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var errSecret = errors.New("password=hunter2 in dsn")

func testServer() *Server { return &Server{Log: slog.New(slog.DiscardHandler)} }

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) APIError {
	t.Helper()
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the error envelope: %v\n%s", err, rec.Body)
	}
	return env.Error
}

func TestHealth(t *testing.T) {
	rec := do(t, testServer().Routes(), http.MethodGet, HealthPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
}

func TestVersionRoute(t *testing.T) {
	rec := do(t, testServer().Routes(), http.MethodGet, APIPrefix+"/version")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct{ Version string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Version == "" {
		t.Errorf("no version in %s (%v)", rec.Body, err)
	}
}

func TestFailuresShareOneShape(t *testing.T) {
	h := testServer().Routes()
	cases := []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, APIPrefix + "/nothing-here", http.StatusNotFound, "not_found"},
		{http.MethodGet, "/nothing-here", http.StatusNotFound, "not_found"},
		{http.MethodPost, HealthPath, http.StatusMethodNotAllowed, "method_not_allowed"},
		{http.MethodDelete, APIPrefix + "/version", http.StatusMethodNotAllowed, "method_not_allowed"},
	}
	for _, c := range cases {
		rec := do(t, h, c.method, c.path)
		if rec.Code != c.status {
			t.Errorf("%s %s: status = %d, want %d", c.method, c.path, rec.Code, c.status)
		}
		got := decodeError(t, rec)
		if got.Code != c.code || got.Message == "" {
			t.Errorf("%s %s: error = %+v, want code %s and a message", c.method, c.path, got, c.code)
		}
		if got.RequestID == "" || got.RequestID != rec.Header().Get(RequestIDHeader) {
			t.Errorf("%s %s: request ID %q in the body, %q in the header", c.method, c.path, got.RequestID, rec.Header().Get(RequestIDHeader))
		}
	}
}

// chain runs a handler behind the same middleware the server uses.
func chain(log *slog.Logger, h HandlerFunc) http.Handler {
	r := chi.NewRouter()
	r.Use(requestID, logging(log), recovery)
	r.Method(http.MethodPost, "/", handle(h))
	return r
}

func TestUnexpectedErrorIsHiddenAndLogged(t *testing.T) {
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	h := chain(log, func(http.ResponseWriter, *http.Request) error {
		return errSecret
	})

	rec := do(t, h, http.MethodPost, "/")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), errSecret.Error()) {
		t.Errorf("the cause reached the client: %s", rec.Body)
	}
	got := decodeError(t, rec)
	if got.Code != "internal" {
		t.Errorf("code = %q, want internal", got.Code)
	}
	if !strings.Contains(logged.String(), errSecret.Error()) || !strings.Contains(logged.String(), got.RequestID) {
		t.Errorf("the log has no line with the cause and the request ID:\n%s", logged.String())
	}
}

func TestPanicIsAnsweredWithTheEnvelope(t *testing.T) {
	h := chain(slog.New(slog.DiscardHandler), func(http.ResponseWriter, *http.Request) error {
		panic("boom")
	})
	rec := do(t, h, http.MethodPost, "/")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeError(t, rec); got.Code != "internal" || strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("error = %+v, body %s", got, rec.Body)
	}
}

func TestRequestIDsDiffer(t *testing.T) {
	h := testServer().Routes()
	a := do(t, h, http.MethodGet, HealthPath).Header().Get(RequestIDHeader)
	b := do(t, h, http.MethodGet, HealthPath).Header().Get(RequestIDHeader)
	if a == "" || a == b {
		t.Errorf("request IDs %q and %q", a, b)
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	cases := []struct {
		body string
		ok   bool
	}{
		{`{"name":"x"}`, true},
		{``, false},
		{`{"name":`, false},
		{`{"name":"x","extra":1}`, false},
		{`{"name":"x"}{"name":"y"}`, false},
		{`{"name":"` + strings.Repeat("x", maxBodyBytes) + `"}`, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(c.body))
		var dst payload
		err := decodeJSON(httptest.NewRecorder(), r, &dst)
		if (err == nil) != c.ok {
			t.Errorf("body %.40q: err = %v, want ok=%v", c.body, err, c.ok)
		}
		if err != nil {
			if apiErr, isAPI := err.(*APIError); !isAPI || apiErr.Status != http.StatusBadRequest {
				t.Errorf("body %.40q: err = %v, want a 400 APIError", c.body, err)
			}
		}
	}
}
