//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

func TestSystemHealthTellsAdministratorsOfTheMachine(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	admin, adminID := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")

	status, out, _ := a.call(admin, http.MethodGet, "/system/health", nil)
	if status != 200 {
		t.Fatalf("health: %d %v", status, out)
	}
	memory := out["memory"].(map[string]any)
	pg := out["postgres"].(map[string]any)
	disk := out["disk"].(map[string]any)
	if memory["machineBytes"].(float64) <= 0 || memory["cpus"].(float64) <= 0 || memory["budgetBytes"].(float64) <= 0 {
		t.Errorf("memory: %v", memory)
	}
	if pg["sharedBuffersBytes"].(float64) < 1<<20 || pg["maxConnections"].(float64) <= 0 || pg["needBytes"].(float64) <= pg["sharedBuffersBytes"].(float64) {
		t.Errorf("postgres: %v", pg)
	}
	if disk["path"] != a.dataDir || disk["totalBytes"].(float64) <= 0 || disk["databaseBytes"].(float64) <= 0 {
		t.Errorf("disk: %v", disk)
	}
	if _, ok := out["warnings"].([]any); !ok {
		t.Errorf("warnings: %v", out["warnings"])
	}
	// Embedding is on by default, with the small model on the CPU.
	if memory["embeddingModel"] != "intfloat/multilingual-e5-small" || memory["embeddingBytes"].(float64) <= 0 {
		t.Errorf("the default model: %v", memory)
	}

	off := "off"
	if err := a.settings.Update(context.Background(), uuid.MustParse(adminID), map[string]*string{settings.EmbeddingEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	_, out, _ = a.call(admin, http.MethodGet, "/system/health", nil)
	if memory := out["memory"].(map[string]any); memory["embeddingModel"] != "" || memory["embeddingBytes"] != 0.0 {
		t.Errorf("embedding off: %v", memory)
	}

	if status, _, _ := a.call(editor, http.MethodGet, "/system/health", nil); status != 403 {
		t.Errorf("an editor: %d", status)
	}
}
