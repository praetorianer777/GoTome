//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// setting finds one setting in the API's list.
func setting(t *testing.T, body map[string]any, key string) map[string]any {
	t.Helper()
	for _, s := range body["settings"].([]any) {
		if st := s.(map[string]any); st["key"] == key {
			return st
		}
	}
	t.Fatalf("no setting %s in %v", key, body)
	return nil
}

func TestSettingsAreForAdministratorsAndSecretsAreWriteOnly(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	const token = "hc-very-secret-token-4711"

	for _, c := range []*http.Client{editor} {
		if status, _, _ := a.call(c, http.MethodGet, "/settings", nil); status != 403 {
			t.Errorf("an editor reads the settings: %d", status)
		}
		if status, _, _ := a.call(c, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.HardcoverToken: "x"}}); status != 403 {
			t.Errorf("an editor changes the settings: %d", status)
		}
	}

	status, body, _ := a.call(admin, http.MethodGet, "/settings", nil)
	if status != 200 || setting(t, body, settings.MetadataLanguage)["value"] != "en" || setting(t, body, settings.HardcoverToken)["isSet"] != false {
		t.Fatalf("the defaults: %d %v", status, body)
	}

	status, body, _ = a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{
		settings.HardcoverToken: "  " + token + "\n", settings.MetadataLanguage: "DE",
	}})
	hc := setting(t, body, settings.HardcoverToken)
	if status != 200 || hc["isSet"] != true || hc["value"] != nil || hc["updatedAt"] == nil ||
		setting(t, body, settings.MetadataLanguage)["value"] != "de" {
		t.Fatalf("after setting them: %d %v", status, body)
	}
	if got, ok, err := a.settings.Secret(ctx, settings.HardcoverToken); err != nil || !ok || got != token {
		t.Errorf("the code that uses the token gets %q %v %v", got, ok, err)
	}
	if lang, err := a.settings.Text(ctx, settings.MetadataLanguage); err != nil || lang != "de" {
		t.Errorf("the language: %q %v", lang, err)
	}

	// The database holds the token sealed only.
	var raw string
	if err := a.pool.QueryRow(ctx, "SELECT coalesce(value, '') || encode(sealed, 'escape') FROM settings WHERE key = $1", settings.HardcoverToken).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, token) {
		t.Error("the database holds the token in clear text")
	}

	// A refused change changes nothing, and does not repeat the secret.
	status, body, _ = a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{
		settings.GoogleBooksKey: token + "-google", settings.MetadataLanguage: "deutsch", "smtp.password": "x",
	}})
	if status != 422 || fieldError(body, settings.MetadataLanguage) == "" || fieldError(body, "smtp.password") == "" {
		t.Errorf("refused: %d %v", status, body)
	}
	if _, ok, _ := a.settings.Secret(ctx, settings.GoogleBooksKey); ok {
		t.Error("half of a refused change was kept")
	}

	// Unset again.
	status, body, _ = a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.HardcoverToken: nil}})
	if status != 200 || setting(t, body, settings.HardcoverToken)["isSet"] != false {
		t.Errorf("unset: %d %v", status, body)
	}

	if logs := a.logs.String(); strings.Contains(logs, token) || logs == "" {
		t.Errorf("the log (%d bytes) holds the token", len(logs))
	}
}

func TestAnotherKeyIsRefusedAtStart(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	if status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{settings.HardcoverToken: "tok"}}); status != 200 {
		t.Fatalf("set: %d %v", status, body)
	}
	// The same key again is fine; another is not.
	if _, err := settings.Open(ctx, a.pool, a.box); err != nil {
		t.Errorf("the same key: %v", err)
	}
	_, err := settings.Open(ctx, a.pool, testBox(t))
	if !errors.Is(err, settings.ErrWrongKey) || !strings.Contains(err.Error(), "GOTOME_SECRET_KEY_FILE") {
		t.Errorf("another key: err = %v, want ErrWrongKey naming the variable", err)
	}
}

func TestSingleSignOnSettingsAreChecked(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, _ := a.signedIn("admin", "admin")
	status, body, _ := a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{
		settings.OIDCIssuer: "auth.example.org", settings.OIDCScopes: "profile email", settings.OIDCDefaultRole: "owner",
		settings.Passwords: "maybe",
	}})
	for _, key := range []string{settings.OIDCIssuer, settings.OIDCScopes, settings.OIDCDefaultRole, settings.Passwords} {
		if status != 422 || fieldError(body, key) == "" {
			t.Errorf("%s: %d %v", key, status, body)
		}
	}
	status, body, _ = a.call(admin, http.MethodPatch, "/settings", map[string]any{"values": map[string]any{
		settings.OIDCIssuer: "https://auth.example.org/realms/home", settings.OIDCScopes: "openid,profile  groups",
		settings.OIDCAdminGroups: " admins , ,Library Admins ", settings.OIDCDefaultRole: "None",
	}})
	if status != 200 || setting(t, body, settings.OIDCScopes)["value"] != "openid profile groups" ||
		setting(t, body, settings.OIDCAdminGroups)["value"] != "admins,Library Admins" ||
		setting(t, body, settings.OIDCDefaultRole)["value"] != "none" {
		t.Errorf("kept as: %d %v", status, body)
	}
}
