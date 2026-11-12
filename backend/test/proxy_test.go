//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// behind serves the app as it is behind a reverse proxy, trusting the
// proxies given, and returns a sign-in that sends what a proxy would.
func behind(t *testing.T, a *app, trusted ...string) func(forwardedFor, proto string, body any) *http.Response {
	t.Helper()
	for _, p := range trusted {
		a.server.TrustedProxies = append(a.server.TrustedProxies, netip.MustParsePrefix(p))
	}
	srv := httptest.NewServer(a.server.Routes())
	t.Cleanup(srv.Close)
	return func(forwardedFor, proto string, body any) *http.Response {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req, err := http.NewRequest(http.MethodPost, srv.URL+httpapi.APIPrefix+"/auth/login", bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", forwardedFor)
		if proto != "" {
			req.Header.Set("X-Forwarded-Proto", proto)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
}

var wrongPassword = map[string]any{"username": "Steve", "password": "not the password"}

// lockOut fails to sign in until the address is refused.
func lockOut(t *testing.T, login func(string, string, any) *http.Response, address string) {
	t.Helper()
	for i := range 5 {
		if resp := login(address, "", wrongPassword); resp.StatusCode != 401 {
			t.Fatalf("attempt %d from %s: %d, want 401", i+1, address, resp.StatusCode)
		}
	}
	if resp := login(address, "", wrongPassword); resp.StatusCode != 429 {
		t.Fatalf("the sixth attempt from %s: %d, want 429", address, resp.StatusCode)
	}
}

func TestClientsBehindATrustedProxyHaveLimitsOfTheirOwn(t *testing.T) {
	a := newApp(t)
	a.call(a.browser(), http.MethodPost, "/setup", steve)
	login := behind(t, a, "127.0.0.0/8", "::1/128")

	lockOut(t, login, "198.51.100.1")
	if resp := login("198.51.100.2", "", steve); resp.StatusCode != 200 {
		t.Errorf("another client behind the proxy: %d, want 200", resp.StatusCode)
	}
	// A client that names another address in front of its own still counts
	// as itself: the proxy appends the address it saw.
	if resp := login("198.51.100.2, 198.51.100.1", "", steve); resp.StatusCode != 429 {
		t.Errorf("the locked-out client naming another address: %d, want 429", resp.StatusCode)
	}

	resp := login("198.51.100.3", "https", steve)
	if cookies := resp.Cookies(); resp.StatusCode != 200 || len(cookies) != 1 || !cookies[0].Secure {
		t.Errorf("TLS at a trusted proxy: %d, cookies %+v, want one Secure cookie", resp.StatusCode, cookies)
	}
}

func TestForwardedHeadersFromAnUntrustedAddressChangeNothing(t *testing.T) {
	a := newApp(t)
	a.call(a.browser(), http.MethodPost, "/setup", steve)
	login := behind(t, a, "10.0.0.0/8")

	lockOut(t, login, "198.51.100.1")
	if resp := login("198.51.100.2", "", steve); resp.StatusCode != 429 {
		t.Errorf("another forwarded address from an untrusted peer: %d, want 429", resp.StatusCode)
	}

	b := newApp(t)
	b.call(b.browser(), http.MethodPost, "/setup", steve)
	login = behind(t, b, "10.0.0.0/8")
	resp := login("198.51.100.3", "https", steve)
	if cookies := resp.Cookies(); resp.StatusCode != 200 || len(cookies) != 1 || cookies[0].Secure {
		t.Errorf("X-Forwarded-Proto from an untrusted peer: %d, cookies %+v, want one cookie, not Secure", resp.StatusCode, cookies)
	}
}
