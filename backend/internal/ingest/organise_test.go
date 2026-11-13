package ingest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/organise"
)

func orgFile(book uuid.UUID, rel, title string) sqlc.ListOrganiseFilesRow {
	return sqlc.ListOrganiseFilesRow{
		FileID: uuid.New(), BookID: book, Kind: "ebook", Format: "epub", RelPath: rel, Title: title,
		Author: "Jane Austen", AuthorSort: "Austen, Jane",
	}
}

func paths(files ...sqlc.ListOrganiseFilesRow) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	for _, f := range files {
		out[f.FileID] = f.RelPath
	}
	return out
}

func TestPlanNumbersTheSecondFileOfASharedNameAndKeepsItThere(t *testing.T) {
	p, _ := organise.Parse("")
	a := orgFile(uuid.New(), "x/a.epub", "Emma")
	b := orgFile(uuid.New(), "x/b.epub", "Emma")
	none := func(string) bool { return false }

	plan := planOrganise(p, []sqlc.ListOrganiseFilesRow{a, b}, paths(a, b), none)
	if len(plan.Moves) != 2 || plan.Collisions != 1 {
		t.Fatalf("plan: %+v", plan)
	}
	got := map[string]bool{plan.Moves[0].To: true, plan.Moves[1].To: true}
	if !got["Jane Austen/Emma.epub"] || !got["Jane Austen/Emma (2).epub"] {
		t.Errorf("targets: %v", got)
	}

	again := planOrganise(p, []sqlc.ListOrganiseFilesRow{a, b}, paths(a, b), none)
	for i := range again.Moves {
		if again.Moves[i].To != plan.Moves[i].To {
			t.Errorf("the plan is not deterministic: %v and %v", again.Moves, plan.Moves)
		}
	}

	a.RelPath, b.RelPath = plan.Moves[0].To, plan.Moves[1].To
	settled := planOrganise(p, []sqlc.ListOrganiseFilesRow{a, b}, paths(a, b), none)
	if len(settled.Moves) != 0 || settled.Unchanged != 2 || settled.Collisions != 1 {
		t.Errorf("a plan over files in place moves: %+v", settled)
	}
}

func TestPlanLeavesAlonePathsOthersHoldOrDiskHas(t *testing.T) {
	p, _ := organise.Parse("")
	mine := orgFile(uuid.New(), "x/a.epub", "Emma")
	other := orgFile(uuid.New(), "Jane Austen/Emma.epub", "Persuasion")
	onDisk := func(rel string) bool { return rel == "Jane Austen/Emma (2).epub" }

	plan := planOrganise(p, []sqlc.ListOrganiseFilesRow{mine}, paths(mine, other), onDisk)
	if len(plan.Moves) != 1 || plan.Moves[0].To != "Jane Austen/Emma (3).epub" {
		t.Errorf("plan: %+v", plan.Moves)
	}
}

func TestPlanSkipsMissingFilesAndCountsPartsInOrder(t *testing.T) {
	p, _ := organise.Parse("")
	book := uuid.New()
	var parts []sqlc.ListOrganiseFilesRow
	for _, rel := range []string{"t/1.mp3", "t/2.mp3", "t/3.mp3"} {
		f := orgFile(book, rel, "Odyssey")
		f.Kind, f.Format = "audio", "mp3"
		parts = append(parts, f)
	}
	gone := orgFile(uuid.New(), "x/gone.epub", "Lost")
	missingSince := time.Now()
	gone.MissingAt = &missingSince
	plan := planOrganise(p, append(parts, gone), paths(append(parts, gone)...), func(string) bool { return false })
	if len(plan.Skipped) != 1 || plan.Skipped[0].Reason != SkipMissing {
		t.Errorf("skipped: %+v", plan.Skipped)
	}
	for i, m := range plan.Moves {
		want := []string{"Jane Austen/Odyssey/01 - Odyssey.mp3", "Jane Austen/Odyssey/02 - Odyssey.mp3", "Jane Austen/Odyssey/03 - Odyssey.mp3"}
		if m.To != want[i] {
			t.Errorf("part %d goes to %s", i+1, m.To)
		}
	}
}

func TestSameFileIsTrueForOneFileUnderTwoNamesOnly(t *testing.T) {
	dir := t.TempDir()
	one, two, link := filepath.Join(dir, "one.epub"), filepath.Join(dir, "two.epub"), filepath.Join(dir, "hard.epub")
	for _, p := range []string{one, two} {
		if err := os.WriteFile(p, []byte("same bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(one, link); err != nil {
		t.Fatal(err)
	}
	soft := filepath.Join(dir, "soft.epub")
	if err := os.Symlink(one, soft); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		a, b string
		want bool
	}{
		{"a file and itself", one, one, true},
		{"a hard link", one, link, true},
		{"another file of the same bytes", one, two, false},
		{"a symlink is its own entry", one, soft, false},
		{"the first is missing", filepath.Join(dir, "gone"), one, false},
		{"the second is missing", one, filepath.Join(dir, "gone"), false},
	} {
		if got := sameFile(c.a, c.b); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}
