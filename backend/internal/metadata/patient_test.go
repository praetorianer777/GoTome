package metadata

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedWeb answers each request with the next of its errors, then with
// a body.
type scriptedWeb struct {
	errs  []error
	asked int
}

func (s *scriptedWeb) Get(context.Context, Request) ([]byte, error) {
	s.asked++
	if s.asked <= len(s.errs) {
		return nil, s.errs[s.asked-1]
	}
	return []byte("ok"), nil
}

func patient(w Web, slept *[]time.Duration) *patientWeb {
	return &patientWeb{
		web:   w,
		waits: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second},
		sleep: func(_ context.Context, d time.Duration) error { *slept = append(*slept, d); return nil },
		log:   func(string, ...any) {},
	}
}

func TestALiveWebAsksAgainAfterAPassingFailure(t *testing.T) {
	now := time.Date(2026, 11, 19, 11, 40, 52, 0, time.UTC)
	reset := fmt.Errorf("GET x: %w", &connectionError{errors.New("connection reset by peer")})
	busy := &BusyError{Provider: "dnb", Until: now.Add(time.Minute)}
	far := &BusyError{Provider: "dnb", Until: now.Add(time.Hour)}

	cases := []struct {
		name  string
		errs  []error
		asked int
		slept []time.Duration
		fails bool
	}{
		{"a reset connection", []error{reset}, 2, []time.Duration{time.Second}, false},
		{"busy, until the time it named", []error{busy}, 2, []time.Duration{time.Minute + time.Second}, false},
		{"busy for long, within the bound", []error{far}, 2, []time.Duration{maxPatience}, false},
		{"one failure after another", []error{reset, busy, reset}, 4, []time.Duration{time.Second, time.Minute + time.Second, 3 * time.Second}, false},
		{"down for good", []error{reset, reset, reset, reset}, 4, []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}, true},
		{"not found is an answer", []error{ErrNotFound}, 1, nil, true},
		{"a refusal is an answer", []error{errors.New("GET x: 403 Forbidden")}, 1, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := &scriptedWeb{errs: c.errs}
			var slept []time.Duration
			p := patient(w, &slept)
			p.now = func() time.Time { return now }
			body, err := p.Get(context.Background(), Request{URL: "https://example.org/"})
			if (err != nil) != c.fails || w.asked != c.asked || fmt.Sprint(slept) != fmt.Sprint(c.slept) {
				t.Errorf("body %q, err %v, asked %d, slept %v", body, err, w.asked, slept)
			}
			if c.fails && !errors.Is(err, c.errs[w.asked-1]) {
				t.Errorf("the last error is not passed on: %v", err)
			}
		})
	}
}

func TestALiveWebStopsWaitingWithItsContext(t *testing.T) {
	w := &scriptedWeb{errs: []error{&connectionError{errors.New("reset")}}}
	p := &patientWeb{web: w, waits: []time.Duration{time.Hour}, sleep: sleep, log: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Get(ctx, Request{}); !errors.Is(err, context.Canceled) || w.asked != 1 {
		t.Errorf("err %v, asked %d", err, w.asked)
	}
}

func TestAConnectionThatBreaksIsAConnectionError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 2 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	web := newWeb(&fakeProvider{name: "fake", base: srv.URL}, newClient(true), nil)

	_, err := web.Get(context.Background(), Request{URL: srv.URL + "/x"})
	var conn *connectionError
	if !errors.As(err, &conn) || !passing(err) {
		t.Fatalf("err = %v", err)
	}
	var slept []time.Duration
	if body, err := patient(web, &slept).Get(context.Background(), Request{URL: srv.URL + "/x"}); err != nil || string(body) != "ok" || len(slept) != 1 {
		t.Errorf("body %q, err %v, slept %v", body, err, slept)
	}
}
