//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
)

func TestTheStatisticsCountWhatTheCallerSees(t *testing.T) {
	t.Parallel()
	a := newApp(t)
	ctx := context.Background()
	admin, _ := a.signedIn("admin", "admin")
	editor, _ := a.signedIn("editor", "editor")
	reader, _ := a.signedIn("reader", "reader")
	open := a.library(admin, "Open", "shared")
	vault := a.library(admin, "Vault", "private")
	for _, up := range []struct{ library, name string }{
		{open, "One.pdf"}, {open, "Two.pdf"}, {vault, "Secret.pdf"},
	} {
		if status, out := a.upload(admin, up.library, up.name, []byte("%PDF-1.4 "+up.name)); status != 200 {
			t.Fatalf("upload %s: %d %v", up.name, status, out)
		}
	}
	// One of the open library's files is read, the other failed; the
	// vault's waits.
	if _, err := a.pool.Exec(ctx, `UPDATE book_files f SET extract_state = CASE WHEN f.rel_path LIKE 'One%' THEN 'done' ELSE 'failed' END,
		updated_at = now() WHERE f.library_id = $1`, open); err != nil {
		t.Fatal(err)
	}

	figures := func(c *http.Client, query string) map[string]any {
		t.Helper()
		status, out, _ := a.call(c, http.MethodGet, "/statistics"+query, nil)
		if status != 200 {
			t.Fatalf("figures%s: %d %v", query, status, out)
		}
		return out
	}
	if got := figures(admin, ""); got["books"] != 3.0 || got["files"] != 3.0 || got["withoutAuthor"] != 3.0 {
		t.Errorf("the administrator: %v", got)
	}
	got := figures(reader, "")
	if got["books"] != 2.0 || got["files"] != 2.0 {
		t.Errorf("the reader, who does not see the vault: %v", got)
	}
	formats := got["formats"].([]any)
	if len(formats) != 1 || formats[0].(map[string]any)["name"] != "pdf" || formats[0].(map[string]any)["count"] != 2.0 {
		t.Errorf("formats: %v", formats)
	}
	if months := got["months"].([]any); len(months) != 1 || months[0].(map[string]any)["count"] != 2.0 {
		t.Errorf("months: %v", months)
	}
	if got := figures(admin, "?library="+vault); got["books"] != 1.0 {
		t.Errorf("the vault alone: %v", got)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/statistics?library="+vault, nil); status != 404 {
		t.Errorf("the vault asked by the reader: %d", status)
	}

	status, work, _ := a.call(editor, http.MethodGet, "/statistics/work", nil)
	if status != 200 {
		t.Fatalf("work: %d %v", status, work)
	}
	files := work["files"].(map[string]any)
	if files["done"] != 2.0 || files["total"] != 2.0 || files["lastHour"] != 2.0 || work["filesFailed"] != 1.0 {
		t.Errorf("the editor's files: %v, failed %v", files, work["filesFailed"])
	}
	status, work, _ = a.call(admin, http.MethodGet, "/statistics/work", nil)
	files = work["files"].(map[string]any)
	if status != 200 || files["done"] != 2.0 || files["total"] != 3.0 || files["minutesLeft"] != 30.0 {
		t.Errorf("the administrator's files: %d %v", status, files)
	}
	if status, _, _ := a.call(reader, http.MethodGet, "/statistics/work", nil); status != 403 {
		t.Errorf("work asked by a reader: %d", status)
	}
}
