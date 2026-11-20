package curation

import "testing"

func TestAnActionOfWholeRowsIsComparedOnTheColumnsRestored(t *testing.T) {
	var annotations Table
	for _, tb := range Tables {
		if tb.Name == "annotations" {
			annotations = tb
		}
	}
	if len(annotations.Columns) == 0 {
		t.Fatal("annotations names no columns")
	}
	left := []map[string]any{
		{"id": "a1", "book_id": "b1", "file_id": "f1", "note": "as merged", "color": "yellow"},
		{"id": "a2", "book_id": "b1", "file_id": nil, "note": "gone since"},
	}
	now := []map[string]any{{"id": "a1", "book_id": "b1", "file_id": "f1"}}
	if n := differing(annotations, left, now); n != 0 {
		t.Errorf("an action recorded with whole rows conflicts on %d rows", n)
	}
	moved := []map[string]any{{"id": "a1", "book_id": "b2", "file_id": "f1"}}
	if n := differing(annotations, left, moved); n != 1 {
		t.Errorf("a highlight moved to another book since: %d rows differ, want 1", n)
	}
}
