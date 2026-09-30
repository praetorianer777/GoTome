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

func TestRoutesNeedASessionUnlessPublic(t *testing.T) {
	h := testServer().Routes()
	rec := do(t, h, http.MethodGet, APIPrefix+"/auth/me")
	if rec.Code != http.StatusUnauthorized || decodeError(t, rec).Code != "unauthorized" {
		t.Errorf("without a session: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, h, http.MethodGet, APIPrefix+"/version"); rec.Code != http.StatusOK {
		t.Errorf("a public route without a session: %d", rec.Code)
	}

	doc := Spec()
	for _, rt := range testServer().routes() {
		op := doc.Paths[rt.Path][map[string]string{"GET": "get", "POST": "post"}[rt.Method]]
		if (len(op.Security) == 0) != rt.Public {
			t.Errorf("%s %s: public %v, but the document's security is %v", rt.Method, rt.Path, rt.Public, op.Security)
		}
	}
	if doc.Components.SecuritySchemes[sessionScheme].Name != SessionCookie {
		t.Error("the document does not describe the session cookie")
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
