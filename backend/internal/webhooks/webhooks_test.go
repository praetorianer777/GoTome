package webhooks

import (
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/praetorianer777/gotome/backend/internal/notify"
)

func TestSignKnownVector(t *testing.T) {
	// Computed independently with Python's hmac module.
	body := []byte(`{"kind":"books.added","data":{"count":2}}`)
	got := Sign([]byte("whsec-test-key-0123456789"), "1700000000", body)
	const want = "sha256=dcb9e0abe3203da9361e7f05d9d77d2c9f908e09d5e3ac55b9723b0f76b18e6a"
	if got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
	if Sign([]byte("whsec-test-key-0123456789"), "1700000001", body) == got {
		t.Error("the timestamp is not part of what is signed")
	}
	if Sign([]byte("another-key-0123456789"), "1700000000", body) == got {
		t.Error("the key is not part of the signature")
	}
	if Sign([]byte("whsec-test-key-0123456789"), "1700000000", append(body, ' ')) == got {
		t.Error("the body is not part of what is signed")
	}
}

func TestBackoffGrowsFourfold(t *testing.T) {
	want := []time.Duration{30 * time.Second, 2 * time.Minute, 8 * time.Minute, 32 * time.Minute}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v, want %v", i+1, got, w)
		}
	}
}

func TestAPayloadHoldsNoNames(t *testing.T) {
	s := &Service{PublicURL: "https://books.example.org/"}
	lib := uuid.New()
	p := s.payloadOf(notify.Event{
		Kind: notify.KindReleaseOut, Libraries: []uuid.UUID{lib}, Link: "/releases",
		Data: map[string]any{
			"title": "The Moonlit Harbour", "authors": []string{"A. Writer"}, "subject": "Harbour Tales",
			"feed": "Some Feed", "error": "open /books/secret: denied", "message": "boom",
			"count": 3, "library": lib.String(), "version": "1.2.3", "security": true,
		},
	})
	raw, _ := json.Marshal(p)
	for _, leak := range []string{"Moonlit", "Writer", "Harbour Tales", "Some Feed", "secret", "boom"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the payload holds %q: %s", leak, raw)
		}
	}
	if p.Data["count"] != 3 || p.Data["version"] != "1.2.3" || p.Data["security"] != true {
		t.Errorf("data = %v", p.Data)
	}
	if p.Link != "https://books.example.org/releases" {
		t.Errorf("link = %q", p.Link)
	}
	if (&Service{}).payloadOf(notify.Event{Kind: notify.KindScanFailed, Link: "/x"}).Link != "/x" {
		t.Error("a link stays relative where no public address is known")
	}
}

func TestInputIsChecked(t *testing.T) {
	good := "0123456789abcdef"
	ok := Input{Name: "n", URL: "https://example.org/hook", Secret: &good, Kinds: []string{"books.added", "books.added"}}
	out, err := clean(ok, true)
	if err != nil || len(out.Kinds) != 1 {
		t.Fatalf("clean: %v %v", out.Kinds, err)
	}
	short := "short"
	for name, in := range map[string]Input{
		"no name":         {URL: ok.URL, Secret: ok.Secret},
		"ftp":             {Name: "n", URL: "ftp://example.org/", Secret: ok.Secret},
		"user in url":     {Name: "n", URL: "https://u:p@example.org/", Secret: ok.Secret},
		"no host":         {Name: "n", URL: "https:///x", Secret: ok.Secret},
		"unknown kind":    {Name: "n", URL: ok.URL, Secret: ok.Secret, Kinds: []string{"nope"}},
		"short secret":    {Name: "n", URL: ok.URL, Secret: &short},
		"test is no kind": {Name: "n", URL: ok.URL, Secret: ok.Secret, Kinds: []string{KindTest}},
	} {
		if _, err := clean(in, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := clean(Input{Name: "n", URL: ok.URL}, true); err == nil {
		t.Error("a webhook without a secret was made")
	}
	if _, err := clean(Input{Name: "n", URL: ok.URL}, false); err != nil {
		t.Errorf("a change without a secret: %v", err)
	}
}

func TestNextRetryWaitsTheBackoffOfTheAttemptThatFailed(t *testing.T) {
	w := &DeliverWorker{}
	for attempt, wait := range map[int]time.Duration{1: 30 * time.Second, 3: 8 * time.Minute} {
		job := &river.Job[DeliverArgs]{JobRow: &rivertype.JobRow{Attempt: attempt}}
		before := time.Now()
		got := w.NextRetry(job)
		if got.Before(before.Add(wait)) || got.After(time.Now().Add(wait)) {
			t.Errorf("attempt %d retried at +%v, want +%v", attempt, got.Sub(before), wait)
		}
	}
}

func TestTheLogFallsBackToTheDefaultLogger(t *testing.T) {
	if (&Service{}).log() != slog.Default() {
		t.Error("a service without a logger must use the default one")
	}
	own := slog.New(slog.NewTextHandler(io.Discard, nil))
	if (&Service{Log: own}).log() != own {
		t.Error("a service's own logger must be used")
	}
}
