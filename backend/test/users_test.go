//go:build integration

package test

import (
	"net/http"
	"testing"
)

// signIn signs an existing account in with its password, in a new browser.
func (a *app) signIn(username, password string) (*http.Client, int) {
	a.t.Helper()
	c := a.browser()
	status, _, _ := a.call(c, http.MethodPost, "/auth/login", map[string]any{"username": username, "password": password})
	return c, status
}

func (a *app) signedInStill(c *http.Client) bool {
	a.t.Helper()
	status, _, _ := a.call(c, http.MethodGet, "/auth/me", nil)
	return status == 200
}

func TestAdministratorsManageAccounts(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")

	if status, _, _ := a.call(editor, http.MethodGet, "/users", nil); status != 403 {
		t.Errorf("an editor lists the users: %d", status)
	}

	status, body, _ := a.call(admin, http.MethodPost, "/users", map[string]any{
		"username": "rita", "password": "a long password", "email": "rita@example.org", "role": "reader",
	})
	if status != 201 || body["role"] != "reader" || body["disabled"] != false {
		t.Fatalf("create: %d %v", status, body)
	}
	rita := body["id"].(string)
	status, body, _ = a.call(admin, http.MethodPost, "/users", map[string]any{
		"username": "RITA", "password": "short", "role": "owner",
	})
	if status != 422 || fieldError(body, "password") == "" || fieldError(body, "role") == "" {
		t.Errorf("a bad account: %d %v", status, body)
	}
	status, _, _ = a.call(admin, http.MethodPost, "/users", map[string]any{
		"username": "RITA", "password": "a long password", "role": "reader",
	})
	if status != 409 {
		t.Errorf("the same name in capitals: %d", status)
	}

	reader, status := a.signIn("rita", "a long password")
	if status != 200 {
		t.Fatalf("the new account signs in: %d", status)
	}
	status, body, _ = a.call(admin, http.MethodPatch, "/users/"+rita, map[string]any{"role": "editor", "email": ""})
	if status != 200 || body["role"] != "editor" || body["email"] != nil {
		t.Errorf("promote: %d %v", status, body)
	}
	_, me, _ := a.call(reader, http.MethodGet, "/auth/me", nil)
	if me["role"] != "editor" {
		t.Errorf("the new role takes effect at once: %v", me)
	}

	// Disabled: signed out, and cannot sign in again.
	status, body, _ = a.call(admin, http.MethodPatch, "/users/"+rita, map[string]any{"disabled": true})
	if status != 200 || body["disabled"] != true {
		t.Fatalf("disable: %d %v", status, body)
	}
	if a.signedInStill(reader) {
		t.Error("a disabled account is still signed in")
	}
	if _, status := a.signIn("rita", "a long password"); status != 401 {
		t.Errorf("a disabled account signs in: %d", status)
	}
	a.call(admin, http.MethodPatch, "/users/"+rita, map[string]any{"disabled": false})
	again, status := a.signIn("rita", "a long password")
	if status != 200 {
		t.Errorf("enabled again, the account signs in: %d", status)
	}

	// A new password from the administrator ends the sessions.
	a.call(admin, http.MethodPatch, "/users/"+rita, map[string]any{"password": "another long one"})
	if a.signedInStill(again) {
		t.Error("the old session outlived the new password")
	}
	if _, status := a.signIn("rita", "another long one"); status != 200 {
		t.Errorf("the new password: %d", status)
	}
	if status, _, _ := a.call(admin, http.MethodDelete, "/users/"+rita+"/sessions", nil); status != 204 {
		t.Errorf("sign out everywhere: %d", status)
	}

	_, body, _ = a.call(admin, http.MethodGet, "/users", nil)
	if users := body["users"].([]any); len(users) != 3 {
		t.Errorf("%d accounts listed, want 3", len(users))
	}
	if status, _, _ := a.call(admin, http.MethodPatch, "/users/00000000-0000-7000-8000-000000000000", map[string]any{"role": "reader"}); status != 404 {
		t.Errorf("no such user: %d", status)
	}
}

func TestTheLastAdministratorStays(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")

	for _, change := range []map[string]any{{"role": "editor"}, {"disabled": true}} {
		status, body, _ := a.call(admin, http.MethodPatch, "/users/"+adminID, change)
		if status != 409 || errorCode(body) != "conflict" {
			t.Errorf("%v on the last administrator: %d %v", change, status, body)
		}
	}

	// With a second administrator, the first may step down; then the second
	// is the last.
	_, body, _ := a.call(admin, http.MethodPost, "/users", map[string]any{"username": "ada", "password": "a long password", "role": "admin"})
	ada := body["id"].(string)
	if status, body, _ := a.call(admin, http.MethodPatch, "/users/"+adminID, map[string]any{"role": "editor"}); status != 200 {
		t.Fatalf("step down beside another administrator: %d %v", status, body)
	}
	other, _ := a.signIn("ada", "a long password")
	if status, _, _ := a.call(other, http.MethodPatch, "/users/"+ada, map[string]any{"disabled": true}); status != 409 {
		t.Errorf("the second, now last, disables herself: %d", status)
	}
}

func TestPeopleManageTheirOwnAccount(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	a.signedIn("reader", "reader")
	laptop, _ := a.signIn("reader", "a long password")
	phone, _ := a.signIn("reader", "a long password")

	status, body, _ := a.call(laptop, http.MethodGet, "/auth/sessions", nil)
	sessions := body["sessions"].([]any)
	if status != 200 || len(sessions) != 3 {
		t.Fatalf("own sessions: %d %v", status, body)
	}
	current := 0
	for _, s := range sessions {
		if s.(map[string]any)["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Errorf("%d sessions marked current", current)
	}

	status, body, _ = a.call(laptop, http.MethodPost, "/auth/password", map[string]any{"currentPassword": "wrong one!", "newPassword": "a new long password"})
	if status != 422 || fieldError(body, "currentPassword") == "" {
		t.Errorf("a wrong current password: %d %v", status, body)
	}
	status, body, _ = a.call(laptop, http.MethodPost, "/auth/password", map[string]any{"currentPassword": "a long password", "newPassword": "short"})
	if status != 422 || fieldError(body, "newPassword") == "" {
		t.Errorf("a short new password: %d %v", status, body)
	}
	if status, _, _ := a.call(laptop, http.MethodPost, "/auth/password", map[string]any{"currentPassword": "a long password", "newPassword": "a new long password"}); status != 204 {
		t.Fatalf("change: %d", status)
	}
	if !a.signedInStill(laptop) || a.signedInStill(phone) {
		t.Errorf("after the change: laptop signed in %v, phone %v; want only the laptop", a.signedInStill(laptop), a.signedInStill(phone))
	}
	if _, status := a.signIn("reader", "a new long password"); status != 200 {
		t.Errorf("the new password: %d", status)
	}

	// Ending one of one's own sessions, and not someone else's.
	_, body, _ = a.call(laptop, http.MethodGet, "/auth/sessions", nil)
	var other string
	for _, s := range body["sessions"].([]any) {
		if s := s.(map[string]any); s["current"] == false {
			other = s["id"].(string)
		}
	}
	stranger, _ := a.signedIn("stranger", "reader")
	if status, _, _ := a.call(stranger, http.MethodDelete, "/auth/sessions/"+other, nil); status != 404 {
		t.Errorf("ending someone else's session: %d", status)
	}
	if status, _, _ := a.call(laptop, http.MethodDelete, "/auth/sessions/"+other, nil); status != 204 {
		t.Errorf("ending one's own: %d", status)
	}
	if status, _, _ := a.call(laptop, http.MethodGet, "/users", nil); status != 403 {
		t.Errorf("a reader lists users: %d", status)
	}
}
