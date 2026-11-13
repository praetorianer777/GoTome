package annotations

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestExportsGroupByBookAndGuardSpreadsheets(t *testing.T) {
	book := uuid.New()
	page := int32(7)
	half, start := 0.5, 0.1
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	list := []Annotation{
		{BookID: book, BookTitle: "Emma", BookAuthors: []string{"Jane Austen"}, Passage: "later", Fraction: &half, CreatedAt: at, Color: "yellow"},
		{BookID: book, BookTitle: "Emma", BookAuthors: []string{"Jane Austen"}, Passage: "=SUM(A1)\nsecond line", Note: "my note", Chapter: "One", Page: &page, Fraction: &start, CreatedAt: at, Color: "green"},
	}
	var md bytes.Buffer
	if err := Markdown(&md, append([]Annotation(nil), list...)); err != nil {
		t.Fatal(err)
	}
	got := md.String()
	if strings.Count(got, "## Emma") != 1 || strings.Index(got, "=SUM") > strings.Index(got, "later") {
		t.Errorf("markdown:\n%s", got)
	}
	if !strings.Contains(got, "> second line") || !strings.Contains(got, "**Note:** my note") || !strings.Contains(got, "One · page 7 · 2026-03-01") {
		t.Errorf("markdown:\n%s", got)
	}
	var csvOut bytes.Buffer
	if err := CSV(&csvOut, list); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(csvOut.String(), "\"'=SUM(A1)\nsecond line\"") || !strings.HasPrefix(csvOut.String(), "Title,Authors,") {
		t.Errorf("csv:\n%s", csvOut.String())
	}
}

func TestChecksRefuseWhatIsNotAnAnnotation(t *testing.T) {
	in := Input{Kind: "pdf", Locator: "page:2", Passage: "  ", Color: "mauve"}
	err := in.check(false).(Problems)
	for _, field := range []string{"passage", "color", "page"} {
		if err[field] == "" {
			t.Errorf("no problem with %s: %v", field, err)
		}
	}
	lenient := Input{Kind: "xpointer", Locator: "/body", Passage: "x", Color: "olive"}
	if err := lenient.check(true); err != nil || lenient.Color != "green" {
		t.Errorf("a device's colour: %v %s", err, lenient.Color)
	}
}
