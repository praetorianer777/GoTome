package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := sameOrigin(ok)

	cases := []struct {
		name, method, origin, fetchSite string
		want                            int
	}{
		{"a read from anywhere", http.MethodGet, "https://evil.example", "cross-site", http.StatusNoContent},
		{"a write from this origin", http.MethodPost, "http://library.example", "", http.StatusNoContent},
		{"this origin over another scheme, behind a proxy", http.MethodPost, "https://library.example", "", http.StatusNoContent},
		{"a write from another site", http.MethodPost, "https://evil.example", "", http.StatusForbidden},
		{"a write from another port", http.MethodPost, "http://library.example:8081", "", http.StatusForbidden},
		{"a sandboxed page", http.MethodPost, "null", "", http.StatusForbidden},
		{"no Origin, the browser says cross-site", http.MethodPost, "", "cross-site", http.StatusForbidden},
		{"no Origin, the browser says same-site", http.MethodDelete, "", "same-site", http.StatusForbidden},
		{"no Origin, the browser says same-origin", http.MethodPost, "", "same-origin", http.StatusNoContent},
		{"not a browser", http.MethodPost, "", "", http.StatusNoContent},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, "http://library.example/api/v1/auth/login", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", c.fetchSite)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, rec.Code, c.want)
		}
	}
}

func TestSessionCookie(t *testing.T) {
	plain := httptest.NewRequest(http.MethodPost, "/", nil)
	direct := httptest.NewRequest(http.MethodPost, "/", nil)
	direct.TLS = &tls.ConnectionState{}
	proxied := httptest.NewRequest(http.MethodPost, "/", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")

	for name, c := range map[string]struct {
		r      *http.Request
		secure bool
	}{"plain HTTP": {plain, false}, "TLS": {direct, true}, "TLS at a proxy": {proxied, true}} {
		rec := httptest.NewRecorder()
		setSessionCookie(rec, c.r, "token-value")
		cookies := rec.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatalf("%s: %d cookies", name, len(cookies))
		}
		got := cookies[0]
		if got.Name != SessionCookie || got.Value != "token-value" || got.Path != "/" {
			t.Errorf("%s: cookie = %+v", name, got)
		}
		if !got.HttpOnly || got.SameSite != http.SameSiteLaxMode || got.Secure != c.secure || got.MaxAge <= 0 {
			t.Errorf("%s: HttpOnly %v, SameSite %v, Secure %v, MaxAge %d", name, got.HttpOnly, got.SameSite, got.Secure, got.MaxAge)
		}
	}

	rec := httptest.NewRecorder()
	clearSessionCookie(rec, plain)
	if got := rec.Result().Cookies()[0]; got.Value != "" || got.MaxAge >= 0 {
		t.Errorf("clearing: cookie = %+v", got)
	}
}

func TestClientAddress(t *testing.T) {
	for remote, want := range map[string]string{
		"192.0.2.7:51234": "192.0.2.7",
		"[2001:db8::1]:8": "2001:db8::1",
		"no-port":         "no-port",
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		if got := clientAddress(r); got != want {
			t.Errorf("clientAddress(%q) = %q, want %q", remote, got, want)
		}
	}
}

func TestAppPathStaysInTheApp(t *testing.T) {
	for in, want := range map[string]string{
		"/books?sort=title":    "/books?sort=title",
		"":                     "/",
		"books":                "/",
		"//evil.example":       "/",
		"/\\evil.example":      "/",
		"https://evil.example": "/",
		"/api/v1/setup":        "/",
		"/a\r\nSet-Cookie:":    "/",
	} {
		if got := appPath(in); got != want {
			t.Errorf("appPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTheCallbackIsWhereTheBrowserReachedTheServer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	r.Host = "books.example:8080"
	if got := callbackURL(r); got != "http://books.example:8080/api/v1/auth/oidc/callback" {
		t.Errorf("plain: %s", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := callbackURL(r); got != "https://books.example:8080/api/v1/auth/oidc/callback" {
		t.Errorf("behind a proxy over TLS: %s", got)
	}
}
