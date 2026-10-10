//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/settings"
	"github.com/praetorianer777/gotome/backend/internal/similar"
)

func TestEmbeddingProgressCountsTheBooksTheCallerSees(t *testing.T) {
	t.Parallel()
	a := newVectorApp(t)
	admin := a.admin
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	// A book of a private library the editor is not in, with its vector.
	vault := a.library(admin, "Vault", "private")
	secret := a.bookIn(vault, "Secretum")
	a.putVector(secret, similar.KindMetadata, direction(map[int]float32{0: 1}))

	status := func(c *http.Client) map[string]any {
		t.Helper()
		code, out, _ := a.call(c, http.MethodGet, "/embedding/status", nil)
		if code != 200 {
			t.Fatalf("status: %d %v", code, out)
		}
		return out
	}
	// The book with text, the bare one and the vault's; the wish is none.
	want := map[string]any{"model": embed.E5Small.Model.Name, "enabled": true, "books": 3.0, "withText": 1.0, "metadata": 1.0, "content": 0.0}
	if got := status(admin); !sameMap(got, want) {
		t.Errorf("before: %v, want %v", got, want)
	}
	if got := status(editor); got["books"] != 2.0 || got["metadata"] != 0.0 {
		t.Errorf("the editor, who does not see the vault: %v", got)
	}
	if code, _, _ := a.call(reader, http.MethodGet, "/embedding/status", nil); code != 403 {
		t.Errorf("a reader: %d", code)
	}

	a.embedAll()
	if got := status(editor); got["metadata"] != 2.0 || got["content"] != 1.0 {
		t.Errorf("after a pass: %v", got)
	}
	a.set(settings.EmbeddingEnabled, "off")
	if got := status(editor); got["enabled"] != false {
		t.Errorf("paused: %v", got)
	}
}
