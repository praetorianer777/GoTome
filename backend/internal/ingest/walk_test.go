package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func write(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkFindsBooksAndNothingElse(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, root, "Dune.EPUB", "dune")
	write(t, root, "Herbert/Dune/01.mp3", "part one")
	write(t, root, "Herbert/cover.jpg", "not a book")
	write(t, root, "Herbert/.hidden.epub", "a hidden file")
	write(t, root, ".trash/Old.epub", "in a hidden folder")
	write(t, root, "@eaDir/Thumb.pdf", "the NAS keeps its own files here")
	write(t, outside, "Secret.epub", "outside the library")
	if err := os.Symlink(filepath.Join(outside, "Secret.epub"), filepath.Join(root, "Link.epub")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Linked")); err != nil {
		t.Fatal(err)
	}

	got, err := walk(context.Background(), root, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range got.files {
		paths = append(paths, f.relPath+" "+f.format)
	}
	want := []string{"Dune.EPUB epub", "Herbert/Dune/01.mp3 mp3"}
	if !slices.Equal(paths, want) {
		t.Errorf("walk found %v, want %v", paths, want)
	}
	if got.files[0].size != 4 {
		t.Errorf("size = %d, want 4", got.files[0].size)
	}
	if got.skipped != 0 || len(got.unreadable) != 0 {
		t.Errorf("skipped %d, unreadable %v, want none", got.skipped, got.unreadable)
	}
}

func TestWalkGoesOnPastAFolderItCannotRead(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	root := t.TempDir()
	write(t, root, "Open/A.epub", "a")
	write(t, root, "Locked/B.epub", "b")
	locked := filepath.Join(root, "Locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	got, err := walk(context.Background(), root, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.files) != 1 || got.files[0].relPath != "Open/A.epub" {
		t.Errorf("files = %v, want Open/A.epub alone", got.files)
	}
	if !slices.Equal(got.unreadable, []string{"Locked"}) || got.skipped != 1 {
		t.Errorf("unreadable = %v, skipped = %d, want Locked and 1", got.unreadable, got.skipped)
	}
	if !below("Locked/B.epub", got.unreadable) || below("LockedToo/B.epub", got.unreadable) {
		t.Error("below does not tell Locked/ from LockedToo/")
	}
}

func TestHashFile(t *testing.T) {
	root := t.TempDir()
	write(t, root, "Dune.epub", "the spice must flow")
	got, sum, err := hashFile(context.Background(), filepath.Join(root, "Dune.epub"), "Dune.epub", "epub")
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("the spice must flow"))
	if !bytes.Equal(sum, want[:]) {
		t.Errorf("hash = %x, want %x", sum, want)
	}
	if got.size != 19 || got.relPath != "Dune.epub" {
		t.Errorf("got %+v", got)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := hashFile(cancelled, filepath.Join(root, "Dune.epub"), "Dune.epub", "epub"); err == nil {
		t.Error("a cancelled hash returned no error")
	}
}
