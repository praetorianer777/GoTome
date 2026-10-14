//go:build integration

package test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/praetorianer777/gotome/backend/internal/httpapi"
	"github.com/praetorianer777/gotome/backend/internal/webui"
)

func TestEveryAnswerCarriesTheSecurityHeaders(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.server.Web = webui.New(fstest.MapFS{"index.html": {Data: []byte("<html>app</html>")}})
	admin, _ := a.signedIn("admin", "admin")
	lib := a.library(admin, "Shelf", "shared")
	emma := filepath.Join(t.TempDir(), "Emma.epub")
	writeEPUB(t, emma, emmaMetadata, nil, "Emma Woodhouse, handsome, clever, and rich.")
	content, err := os.ReadFile(emma)
	if err != nil {
		t.Fatal(err)
	}
	status, uploaded := a.upload(admin, lib, "Emma.epub", content)
	if status != 200 {
		t.Fatalf("upload: %d %v", status, uploaded)
	}

	get := func(path string) http.Header {
		t.Helper()
		resp, err := admin.Get(a.url + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header
	}
	common := func(what string, h http.Header) {
		t.Helper()
		for name, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "SAMEORIGIN",
			"Referrer-Policy":        "same-origin",
		} {
			if got := h.Get(name); got != want {
				t.Errorf("%s: %s is %q, want %q", what, name, got, want)
			}
		}
		if !strings.Contains(h.Get("Permissions-Policy"), "camera=()") {
			t.Errorf("%s: Permissions-Policy is %q", what, h.Get("Permissions-Policy"))
		}
	}
	// The API's answers are not pages: whatever a browser makes of one runs
	// nothing and loads nothing.
	sandboxed := func(what string, h http.Header) {
		t.Helper()
		common(what, h)
		csp := h.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: Content-Security-Policy is %q", what, csp)
		}
	}

	page := get("/books/42")
	common("a page of the app", page)
	if got := page.Get("Content-Security-Policy"); got != webui.ContentSecurityPolicy {
		t.Errorf("a page of the app has the policy %q", got)
	}

	sandboxed("an answer of the API", get(httpapi.APIPrefix+"/libraries"))
	sandboxed("an error of the API", get(httpapi.APIPrefix+"/nothing"))
	sandboxed("the health check", get(httpapi.HealthPath))

	file := get(httpapi.APIPrefix + "/files/" + uploaded["fileId"].(string) + "/download")
	sandboxed("a book's file", file)
	if got := file.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
		t.Errorf("a book's file is sent as %q, not as an attachment", got)
	}
	if got := file.Get("Content-Type"); got != "application/epub+zip" {
		t.Errorf("a book's file is sent as %q", got)
	}
}

// The identity provider an administrator set up may be on the local
// network, and is reached there; an address inside that its discovery
// document names besides is not.
func TestTheIdentityProviderLeadsNowhereElseInside(t *testing.T) {
	a := newSSOApp(t, nil)
	var reached atomic.Int32
	inside := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))
	t.Cleanup(inside.Close)
	a.idp.SendTokensTo(inside.URL + "/token")

	c := a.browser()
	landed := a.signInVia(c, alice)
	if !strings.HasPrefix(landed, "/login?sso=failed") || a.me(c) != nil {
		t.Errorf("a sign-in whose tokens come from inside landed on %q", landed)
	}
	if reached.Load() != 0 {
		t.Errorf("the address inside was reached %d times", reached.Load())
	}
	if !strings.Contains(a.logs.String(), "not on the public internet") {
		t.Error("the log does not say why the sign-in failed")
	}
}
