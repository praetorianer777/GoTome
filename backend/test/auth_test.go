//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/covers"
	"github.com/praetorianer777/gotome/backend/internal/db/dbtest"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/reading"
	"github.com/praetorianer777/gotome/backend/internal/releases"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/secret"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
	"github.com/praetorianer777/gotome/backend/internal/similar"
	"github.com/praetorianer777/gotome/backend/internal/sso"
)

// Cheap hashing: these tests sign in dozens of times.
var fastHash = auth.PasswordParams{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}

// uploadLimit is small, so that a test can go past it cheaply.
const uploadLimit = 1 << 20

// lockedBuffer is a log that requests on several goroutines write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func testBox(t *testing.T) *secret.Box {
	t.Helper()
	encoded, err := secret.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	key, err := secret.ParseKey(encoded)
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// app is a server on a database of its own, with a clock the test moves.
type app struct {
	t    *testing.T
	url  string
	pool *pgxpool.Pool
	// dataDir is where this app's managed libraries are created.
	dataDir string
	scans   *ingest.Service
	// server is the app's; a test may give it what most tests do without,
	// such as metadata providers, before its first request.
	server *httpapi.Server
	covers *covers.Store
	// box seals this app's secrets; settings keeps them.
	box      *secret.Box
	settings *settings.Store
	logs     lockedBuffer
	// works is what the providers list for following authors and series.
	works *fakeWorks
	now   time.Time
	mu    sync.Mutex
}

func (a *app) clock() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.now
}

func (a *app) advance(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.now = a.now.Add(d)
}

func newApp(t *testing.T) *app {
	t.Helper()
	return newAppOn(t, dbtest.New(t))
}

// newAppOn is newApp on a pool the test made, such as one that reports the
// plans of the queries it runs.
func newAppOn(t *testing.T, pool *pgxpool.Pool) *app {
	t.Helper()
	a := &app{t: t, pool: pool, now: time.Now()}
	accounts, err := auth.NewService(a.pool, fastHash, auth.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	a.dataDir = t.TempDir()
	quiet := slog.New(slog.DiscardHandler)
	libraries := library.NewService(a.pool, a.dataDir)
	a.covers = covers.NewStore(a.dataDir)
	a.scans = ingest.NewService(a.pool, libraries, a.covers, quiet)
	a.scans.UploadLimit = uploadLimit
	// Jobs are queued and left there; a test that wants them worked calls
	// workJobs.
	queue, err := jobs.New(a.pool, jobs.Config{Logger: quiet, InsertOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	a.scans.Queue = queue
	a.box = testBox(t)
	a.settings, err = settings.Open(context.Background(), a.pool, a.box)
	if err != nil {
		t.Fatal(err)
	}
	a.server = &httpapi.Server{
		// Everything the server logs, so that a test can look for what must
		// not be in it.
		Log:       slog.New(slog.NewTextHandler(&a.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Settings:  a.settings,
		DB:        a.pool,
		Auth:      accounts.WithClock(a.clock),
		Logins:    httpapi.NewLoginLimits(a.clock),
		Libraries: libraries,
		Scans:     a.scans,
		Books:     catalog.NewService(a.pool),
		Covers:    a.covers,
	}
	a.server.Auth.PasswordsOff = a.settings.PasswordsOff
	a.server.SSO = sso.NewService(a.pool, a.server.Auth, a.settings)
	a.server.Bulk = bulk.NewService(a.pool, a.scans, matchesOf{a}, quiet)
	a.server.Bulk.Queue = queue
	a.server.Reading = reading.NewService(a.pool)
	a.server.Shelves = shelves.NewService(a.pool, a.server.Books)
	a.server.Search = search.NewPGSearch(a.pool, a.server.Books)
	a.server.Duplicates = dedupe.NewService(a.pool, a.server.Books, quiet)
	a.server.Duplicates.Queue = queue
	a.server.Similar = similar.NewService(a.pool, a.settings.Embedding, nil, quiet)
	a.server.Index = search.NewIndex(a.pool, quiet)
	a.server.Index.Queue = queue
	a.works = &fakeWorks{}
	a.server.Releases = releases.NewService(a.pool, a.works, quiet)
	a.server.Releases.Queue = queue
	a.server.Notifications = notify.NewService(a.pool)
	a.server.NotifyHub = notify.NewHub(a.pool, quiet)
	hubCtx, stopHub := context.WithCancel(context.Background())
	t.Cleanup(stopHub)
	go a.server.NotifyHub.Run(hubCtx)
	srv := httptest.NewServer(a.server.Routes())
	t.Cleanup(srv.Close)
	a.url = srv.URL
	return a
}

// browser is a client that keeps cookies, as one person's browser does.
func (a *app) browser() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		a.t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// call sends JSON and returns the status and the decoded body.
func (a *app) call(c *http.Client, method, path string, body any) (int, map[string]any, http.Header) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, a.url+httpapi.APIPrefix+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			a.t.Fatalf("%s %s: body is not JSON: %s", method, path, raw)
		}
	}
	return resp.StatusCode, decoded, resp.Header
}

// signedIn creates an account with the role and returns a browser signed in
// as it. The account goes straight into the database: creating users through
// the API is its own feature.
func (a *app) signedIn(username, role string) (*http.Client, string) {
	a.t.Helper()
	const password = "a long password"
	hash, err := auth.HashPassword(password, fastHash)
	if err != nil {
		a.t.Fatal(err)
	}
	var id string
	err = a.pool.QueryRow(context.Background(),
		"INSERT INTO users (username, password_hash, role) VALUES ($1, $2, $3) RETURNING id::text",
		username, hash, role).Scan(&id)
	if err != nil {
		a.t.Fatal(err)
	}
	c := a.browser()
	if status, body, _ := a.call(c, http.MethodPost, "/auth/login", map[string]any{"username": username, "password": password}); status != 200 {
		a.t.Fatalf("sign in as %s: %d %v", username, status, body)
	}
	return c, id
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

var steve = map[string]any{"username": "Steve", "password": "a long password"}

func TestSetupWorksExactlyOnce(t *testing.T) {
	a := newApp(t)
	c := a.browser()

	if status, body, _ := a.call(c, http.MethodGet, "/setup", nil); status != 200 || body["needed"] != true {
		t.Fatalf("before setup: %d %v", status, body)
	}
	if status, body, _ := a.call(c, http.MethodPost, "/setup", map[string]any{"username": "", "password": "short"}); status != 422 || errorCode(body) != "validation_failed" {
		t.Errorf("invalid details: %d %v", status, body)
	}

	status, body, _ := a.call(c, http.MethodPost, "/setup", steve)
	if status != 201 || body["username"] != "Steve" || body["role"] != "admin" {
		t.Fatalf("setup: %d %v", status, body)
	}
	if _, leaked := body["password_hash"]; leaked || body["passwordHash"] != nil {
		t.Errorf("the account's hash is in the answer: %v", body)
	}
	// Setup signs the new administrator in.
	if status, me, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 200 || me["username"] != "Steve" {
		t.Errorf("after setup: %d %v", status, me)
	}

	if status, body, _ := a.call(c, http.MethodGet, "/setup", nil); status != 200 || body["needed"] != false {
		t.Errorf("after setup: %d %v", status, body)
	}
	other := map[string]any{"username": "mallory", "password": "another long one"}
	if status, body, _ := a.call(a.browser(), http.MethodPost, "/setup", other); status != 409 || errorCode(body) != "conflict" {
		t.Errorf("a second setup: %d %v", status, body)
	}
}

func TestConcurrentSetupMakesOneAdministrator(t *testing.T) {
	a := newApp(t)
	const attempts = 8
	statuses := make([]int, attempts)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Go(func() {
			details := map[string]any{"username": "admin" + strconv.Itoa(i), "password": "a long password"}
			statuses[i], _, _ = a.call(a.browser(), http.MethodPost, "/setup", details)
		})
	}
	wg.Wait()

	created := 0
	for _, status := range statuses {
		switch status {
		case 201:
			created++
		case 409:
		default:
			t.Errorf("status %d, want 201 or 409", status)
		}
	}
	var users int
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&users); err != nil {
		t.Fatal(err)
	}
	if created != 1 || users != 1 {
		t.Errorf("%d requests succeeded and %d accounts exist, want one of each", created, users)
	}
}

func TestLoginLogout(t *testing.T) {
	a := newApp(t)
	a.call(a.browser(), http.MethodPost, "/setup", steve)
	c := a.browser()

	if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 401 {
		t.Errorf("before signing in: %d, want 401", status)
	}
	for name, creds := range map[string]map[string]any{
		"wrong password": {"username": "Steve", "password": "not the password"},
		"unknown user":   {"username": "nobody", "password": "a long password"},
	} {
		status, body, _ := a.call(c, http.MethodPost, "/auth/login", creds)
		if status != 401 || errorCode(body) != "unauthorized" {
			t.Errorf("%s: %d %v", name, status, body)
		}
	}

	// The user name is case-insensitive.
	status, body, header := a.call(c, http.MethodPost, "/auth/login", map[string]any{"username": "steve", "password": "a long password"})
	if status != 200 || body["username"] != "Steve" {
		t.Fatalf("login: %d %v", status, body)
	}
	cookie := header.Get("Set-Cookie")
	for _, attr := range []string{httpapi.SessionCookie + "=", "HttpOnly", "SameSite=Lax", "Path=/"} {
		if !bytes.Contains([]byte(cookie), []byte(attr)) {
			t.Errorf("Set-Cookie %q lacks %s", cookie, attr)
		}
	}
	status, me, _ := a.call(c, http.MethodGet, "/auth/me", nil)
	if status != 200 || me["role"] != "admin" {
		t.Errorf("signed in: %d %v", status, me)
	}
	// The answer says what the role allows, so the web app need not guess.
	permissions, _ := me["permissions"].([]any)
	if !slices.Contains(permissions, any("users:manage")) || !slices.Contains(permissions, any("library:read")) {
		t.Errorf("an administrator's permissions = %v", permissions)
	}

	// The database holds a hash of the token, never the token.
	var stored int
	token := cookieValue(t, c, a.url)
	if err := a.pool.QueryRow(context.Background(), "SELECT count(*) FROM sessions WHERE token_hash = $1", []byte(token)).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("the token itself is stored: %d, %v", stored, err)
	}

	if status, _, _ := a.call(c, http.MethodPost, "/auth/logout", nil); status != 204 {
		t.Errorf("logout: %d", status)
	}
	if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 401 {
		t.Errorf("after signing out: %d, want 401", status)
	}
	// The token is dead on the server, not only forgotten by this browser.
	replay := a.browser()
	req, _ := http.NewRequest(http.MethodGet, a.url+httpapi.APIPrefix+"/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: token})
	resp, err := replay.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("a replayed token after logout: %d, want 401", resp.StatusCode)
	}
}

func cookieValue(t *testing.T, c *http.Client, rawURL string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
	for _, cookie := range c.Jar.Cookies(req.URL) {
		if cookie.Name == httpapi.SessionCookie {
			return cookie.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestSessionExpiresWhenUnusedAndLastsWhileUsed(t *testing.T) {
	a := newApp(t)
	c := a.browser()
	a.call(c, http.MethodPost, "/setup", steve)

	// Used every twenty days for a hundred days: never thirty days idle.
	for range 5 {
		a.advance(20 * 24 * time.Hour)
		if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 200 {
			t.Fatalf("a session in use ended: %d", status)
		}
	}
	a.advance(auth.DefaultSessionTTL + time.Minute)
	if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 401 {
		t.Errorf("a session unused for longer than its lifetime: %d, want 401", status)
	}
}

func TestDisabledUserIsSignedOutAndCannotSignIn(t *testing.T) {
	a := newApp(t)
	c := a.browser()
	a.call(c, http.MethodPost, "/setup", steve)

	if _, err := a.pool.Exec(context.Background(), "UPDATE users SET disabled_at = now()"); err != nil {
		t.Fatal(err)
	}
	if status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil); status != 401 {
		t.Errorf("an open session of a disabled user: %d, want 401", status)
	}
	if status, _, _ := a.call(a.browser(), http.MethodPost, "/auth/login", steve); status != 401 {
		t.Errorf("a disabled user signing in: %d, want 401", status)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	a := newApp(t)
	a.call(a.browser(), http.MethodPost, "/setup", steve)
	c := a.browser()
	wrong := map[string]any{"username": "Steve", "password": "not the password"}

	for i := range 5 {
		if status, _, _ := a.call(c, http.MethodPost, "/auth/login", wrong); status != 401 {
			t.Fatalf("attempt %d: %d, want 401", i+1, status)
		}
	}
	status, body, header := a.call(c, http.MethodPost, "/auth/login", wrong)
	if status != 429 || errorCode(body) != "too_many_requests" {
		t.Fatalf("the sixth attempt: %d %v", status, body)
	}
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err != nil || seconds <= 0 {
		t.Errorf("Retry-After = %q", header.Get("Retry-After"))
	}
	// Blocked means blocked: the right password does not get through either.
	if status, _, _ := a.call(c, http.MethodPost, "/auth/login", steve); status != 429 {
		t.Errorf("the right password while blocked: %d, want 429", status)
	}

	a.advance(6 * time.Minute)
	if status, _, _ := a.call(c, http.MethodPost, "/auth/login", steve); status != 200 {
		t.Errorf("after the window: %d, want 200", status)
	}
}

func TestCrossSiteWriteIsRefused(t *testing.T) {
	a := newApp(t)
	body, _ := json.Marshal(steve)
	req, _ := http.NewRequest(http.MethodPost, a.url+httpapi.APIPrefix+"/setup", bytes.NewReader(body))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("setup from another site: %d, want 403", resp.StatusCode)
	}
	if status, status2, _ := a.call(a.browser(), http.MethodGet, "/setup", nil); status != 200 || status2["needed"] != true {
		t.Errorf("the refused request changed something: %d %v", status, status2)
	}
}

func toJSON(v any) string {
	encoded, _ := json.Marshal(v)
	return string(encoded)
}
