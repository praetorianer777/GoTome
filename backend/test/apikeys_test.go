//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func keyClient(token string) *http.Client { return &http.Client{Transport: bearer{token}} }

func (a *app) makeKey(c *http.Client, name string, scopes ...string) (id, token string) {
	a.t.Helper()
	status, out, _ := a.call(c, http.MethodPost, "/auth/api-keys", map[string]any{"name": name, "scopes": scopes})
	if status != 201 {
		a.t.Fatalf("make key %s %v: %d %v", name, scopes, status, out)
	}
	return out["key"].(map[string]any)["id"].(string), out["token"].(string)
}

func TestAnAPIKeyReachesOnlyItsScopesAndNeverMoreThanItsOwner(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	reader, _ := a.signedIn("reader", "reader")

	routes := map[string]struct{ method, path string }{
		"read":     {http.MethodGet, "/books"},
		"progress": {http.MethodGet, "/me/stats"},
		"upload":   {http.MethodPost, "/libraries/00000000-0000-0000-0000-000000000000/uploads"},
		"admin":    {http.MethodGet, "/users"},
	}
	for scope := range routes {
		_, token := a.makeKey(admin, "only "+scope, scope)
		key := keyClient(token)
		for other, r := range routes {
			status, _, _ := a.call(key, r.method, r.path, nil)
			if other == scope && (status == 401 || status == 403) {
				t.Errorf("a %q key on %s %s: %d", scope, r.method, r.path, status)
			}
			if other != scope && status != 403 {
				t.Errorf("a %q key on the %q route: %d, want 403", scope, other, status)
			}
		}
		if status, _, _ := a.call(key, http.MethodGet, "/auth/api-keys", nil); status != 403 {
			t.Errorf("a %q key lists keys: %d", scope, status)
		}
		if status, _, _ := a.call(key, http.MethodPost, "/auth/api-keys", map[string]any{"name": "more", "scopes": []string{scope}}); status != 403 {
			t.Errorf("a %q key makes a key: %d", scope, status)
		}
		if status, _, _ := a.call(key, http.MethodGet, "/auth/sessions", nil); status != 403 {
			t.Errorf("a %q key lists sessions: %d", scope, status)
		}
	}

	_, token := a.makeKey(admin, "me", "read")
	status, me, _ := a.call(keyClient(token), http.MethodGet, "/auth/me", nil)
	perms, _ := me["permissions"].([]any)
	if status != 200 || len(perms) != 1 || perms[0] != "library:read" {
		t.Errorf("/auth/me with a read key: %d %v", status, me)
	}

	for _, scope := range []string{"edit", "upload", "admin"} {
		status, out, _ := a.call(reader, http.MethodPost, "/auth/api-keys", map[string]any{"name": "x", "scopes": []string{scope}})
		if status != 422 {
			t.Errorf("a reader makes a %q key: %d %v", scope, status, out)
		}
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/auth/api-keys", map[string]any{"name": "x", "scopes": []string{"root"}}); status != 422 {
		t.Errorf("an unknown scope: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodPost, "/auth/api-keys", map[string]any{"name": "x", "scopes": []string{}}); status != 422 {
		t.Errorf("no scope: %d", status)
	}
}

func TestAnAPIKeyIsStoredHashedShownOnceAndRevoked(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")
	other, _ := a.signedIn("other", "reader")
	id, token := a.makeKey(reader, "script", "read")

	var stored int
	if err := a.pool.QueryRow(t.Context(), "SELECT count(*) FROM api_keys WHERE key_hash = sha256($1::bytea)", token).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("the hash of the key: %d %v", stored, err)
	}
	var leaked int
	if err := a.pool.QueryRow(t.Context(), "SELECT count(*) FROM api_keys WHERE key_prefix = $1 OR name = $1", token).Scan(&leaked); err != nil || leaked != 0 {
		t.Errorf("the key itself is stored: %d %v", leaked, err)
	}

	_, list, _ := a.call(reader, http.MethodGet, "/auth/api-keys", nil)
	keys := list["keys"].([]any)
	if len(keys) != 1 || strings.Contains(keys[0].(map[string]any)["prefix"].(string), token[12:]) {
		t.Fatalf("the list: %v", list)
	}
	if _, has := keys[0].(map[string]any)["token"]; has {
		t.Error("the list holds the token")
	}
	if keys[0].(map[string]any)["lastUsedAt"] != nil {
		t.Error("used before it was used")
	}

	key := keyClient(token)
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 200 {
		t.Fatalf("the key: %d", status)
	}
	_, list, _ = a.call(reader, http.MethodGet, "/auth/api-keys", nil)
	if list["keys"].([]any)[0].(map[string]any)["lastUsedAt"] == nil {
		t.Error("last use is not recorded")
	}

	if status, _, _ := a.call(other, http.MethodDelete, "/auth/api-keys/"+id, nil); status != 404 {
		t.Errorf("another person revokes it: %d", status)
	}
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 200 {
		t.Errorf("after another's attempt: %d", status)
	}
	if status, _, _ := a.call(reader, http.MethodDelete, "/auth/api-keys/"+id, nil); status != 204 {
		t.Fatalf("revoke: %d", status)
	}
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 401 {
		t.Errorf("revoked: %d", status)
	}
}

func TestAnAPIKeyExpires(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	reader, _ := a.signedIn("reader", "reader")
	status, out, _ := a.call(reader, http.MethodPost, "/auth/api-keys", map[string]any{
		"name": "past", "scopes": []string{"read"}, "expiresAt": a.clock().Add(-time.Hour),
	})
	if status != 422 {
		t.Errorf("an expiry in the past: %d %v", status, out)
	}
	status, out, _ = a.call(reader, http.MethodPost, "/auth/api-keys", map[string]any{
		"name": "short", "scopes": []string{"read"}, "expiresAt": a.clock().Add(time.Hour),
	})
	if status != 201 {
		t.Fatalf("make: %d %v", status, out)
	}
	key := keyClient(out["token"].(string))
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 200 {
		t.Errorf("before expiry: %d", status)
	}
	a.advance(2 * time.Hour)
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 401 {
		t.Errorf("after expiry: %d", status)
	}
}

func TestAnAPIKeyOfADisabledOrDemotedAccountStopsOrNarrows(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, editorID := a.signedIn("editor", "editor")
	_, token := a.makeKey(editor, "script", "read", "upload")
	key := keyClient(token)
	upload := "/libraries/00000000-0000-0000-0000-000000000000/uploads"

	if status, _, _ := a.call(key, http.MethodPost, upload, nil); status == 401 || status == 403 {
		t.Fatalf("an editor's upload key: %d", status)
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/users/"+editorID, map[string]any{"role": "reader"}); status != 200 {
		t.Fatalf("demote: %d %v", status, out)
	}
	if status, _, _ := a.call(key, http.MethodPost, upload, nil); status != 403 {
		t.Errorf("a demoted owner's upload key: %d", status)
	}
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 200 {
		t.Errorf("a demoted owner's read key: %d", status)
	}
	if status, out, _ := a.call(admin, http.MethodPatch, "/users/"+editorID, map[string]any{"disabled": true}); status != 200 {
		t.Fatalf("disable: %d %v", status, out)
	}
	if status, _, _ := a.call(key, http.MethodGet, "/books", nil); status != 401 {
		t.Errorf("a disabled account's key: %d", status)
	}
}

func TestGuessingAnAPIKeyIsSlowedDown(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	guess := keyClient("gotome_" + strings.Repeat("A", 43))
	var status int
	for range 6 {
		status, _, _ = a.call(guess, http.MethodGet, "/books", nil)
	}
	if status != 429 {
		t.Errorf("the sixth wrong key: %d", status)
	}
}
