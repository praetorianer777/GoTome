//go:build integration

package test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/outbound"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/updates"
)

type fakeGitHub struct {
	status   atomic.Int32
	body     atomic.Value
	requests atomic.Int32
	agent    atomic.Value
	auth     atomic.Value
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, string) {
	t.Helper()
	g := &fakeGitHub{}
	g.status.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.requests.Add(1)
		g.agent.Store(r.UserAgent())
		g.auth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(int(g.status.Load()))
		w.Write([]byte(g.body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	return g, srv.URL
}

func (g *fakeGitHub) release(tag, notes string, flags map[string]bool) {
	b, _ := json.Marshal(map[string]any{
		"tag_name": tag, "draft": flags["draft"], "prerelease": flags["prerelease"],
		"published_at": "2026-11-01T10:00:00Z", "body": notes,
	})
	g.body.Store(string(b))
}

func TestANewReleaseIsToldOncePerVersionToAdministratorsOnly(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	events := a.withEvents()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	github, url := newFakeGitHub(t)
	clock := time.Now()
	svc := updates.NewService(a.pool, a.settings, events, slog.New(slog.DiscardHandler))
	svc.HTTP = outbound.NewClient(outbound.Options{AllowPrivate: true, Timeout: 5 * time.Second})
	svc.URL, svc.Current = url, "0.12.0"
	svc.Now = func() time.Time { return clock }
	a.server.Updates = svc

	check := func() bool {
		t.Helper()
		asked, err := svc.Check(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		a.flush(events)
		return asked
	}
	updateNotes := func(c *http.Client) int {
		n := 0
		for _, v := range a.told(c) {
			if v.Kind == notify.KindUpdateAvailable {
				n++
			}
		}
		return n
	}

	github.release("v0.12.0", "", nil)
	check()
	if updateNotes(admin) != 0 {
		t.Error("the running version was announced as an update")
	}
	if got := github.agent.Load(); got != "GOtome/0.12.0 (+https://github.com/praetorianer777/GoTome)" {
		t.Errorf("user agent %v", got)
	}
	if got := github.auth.Load(); got != "" {
		t.Errorf("sent authorization %v", got)
	}

	if check() {
		t.Error("GitHub was asked again within the day")
	}

	clock = clock.Add(25 * time.Hour)
	github.release("v0.11.0", "", nil)
	check()
	clock = clock.Add(25 * time.Hour)
	github.release("v0.14.0-rc1", "", map[string]bool{"prerelease": true})
	check()
	clock = clock.Add(2 * time.Hour)
	github.release("v0.14.0", "", map[string]bool{"draft": true})
	check()
	if updateNotes(admin) != 0 {
		t.Error("an older, pre-release or draft version was announced")
	}

	clock = clock.Add(25 * time.Hour)
	github.release("v0.13.0", "## Added\n- x\n\n### Security\n- y\n", nil)
	check()
	status, out, _ := a.call(admin, http.MethodGet, "/system/update", nil)
	if status != 200 || out["latest"] != "0.13.0" || out["available"] != true || out["security"] != true || out["current"] != "0.12.0" {
		t.Fatalf("status %d %v", status, out)
	}
	if out["url"] != "https://github.com/praetorianer777/GoTome/releases/tag/v0.13.0" {
		t.Errorf("url %v", out["url"])
	}
	if updateNotes(admin) != 1 || updateNotes(editor) != 0 {
		t.Errorf("admin told %d, editor %d", updateNotes(admin), updateNotes(editor))
	}
	if status, _, _ := a.call(editor, http.MethodGet, "/system/update", nil); status != 403 {
		t.Errorf("an editor reads the update status: %d", status)
	}

	// Asked again, the same version is not told again; a failure keeps the
	// last answer.
	clock = clock.Add(25 * time.Hour)
	check()
	clock = clock.Add(25 * time.Hour)
	github.status.Store(500)
	check()
	if updateNotes(admin) != 1 {
		t.Errorf("the version was told %d times", updateNotes(admin))
	}
	_, out, _ = a.call(admin, http.MethodGet, "/system/update", nil)
	if out["latest"] != "0.13.0" || out["available"] != true {
		t.Errorf("a failed check lost the answer: %v", out)
	}

	github.status.Store(200)
	clock = clock.Add(2 * time.Hour)
	github.release("v0.13.1", "", nil)
	check()
	if updateNotes(admin) != 2 {
		t.Errorf("a further version was told %d times in all", updateNotes(admin))
	}
}

func TestADevelopmentBuildNeverReportsAnUpdate(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	github, url := newFakeGitHub(t)
	github.release("v9.0.0", "", nil)
	svc := updates.NewService(a.pool, a.settings, a.withEvents(), slog.New(slog.DiscardHandler))
	svc.HTTP = outbound.NewClient(outbound.Options{AllowPrivate: true, Timeout: 5 * time.Second})
	svc.URL, svc.Current = url, "dev"
	a.server.Updates = svc
	admin, _ := a.signedIn("admin", "admin")
	if _, err := svc.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, out, _ := a.call(admin, http.MethodGet, "/system/update", nil)
	if out["available"] != false || out["latest"] != "9.0.0" {
		t.Errorf("status %v", out)
	}
}

func TestNothingIsAskedOfflineOrWithTheCheckOff(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	github, url := newFakeGitHub(t)
	github.release("v9.0.0", "", nil)
	svc := updates.NewService(a.pool, a.settings, a.withEvents(), slog.New(slog.DiscardHandler))
	svc.HTTP = outbound.NewClient(outbound.Options{AllowPrivate: true, Timeout: 5 * time.Second})
	svc.URL, svc.Current = url, "0.12.0"

	svc.Offline = true
	if asked, err := svc.Check(t.Context()); err != nil || asked {
		t.Errorf("offline: asked %v, error %v", asked, err)
	}
	svc.Offline = false
	_, adminID := a.signedIn("admin", "admin")
	off := "off"
	if err := a.settings.Update(t.Context(), uuid.MustParse(adminID), map[string]*string{settings.UpdatesCheck: &off}); err != nil {
		t.Fatal(err)
	}
	if asked, err := svc.Check(t.Context()); err != nil || asked {
		t.Errorf("off: asked %v, error %v", asked, err)
	}
	if github.requests.Load() != 0 {
		t.Errorf("%d requests", github.requests.Load())
	}
}
