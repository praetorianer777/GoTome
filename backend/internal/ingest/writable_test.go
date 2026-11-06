package ingest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/uuid"
)

func TestARefusedWriteIsAFolderThatCannotBeWritten(t *testing.T) {
	lib := uuid.New()
	root := "/books"
	tests := []struct {
		name   string
		err    error
		folder string
		reason string
	}{
		{
			name:   "mkdir without permission",
			err:    &fs.PathError{Op: "mkdir", Path: "/books/.trash", Err: syscall.EACCES},
			folder: "/books", reason: "permission denied",
		},
		{
			name:   "a file not allowed",
			err:    fmt.Errorf("write Emma.epub: %w", &fs.PathError{Op: "open", Path: "/books/Austen/.Emma.epub.gotome-fix", Err: syscall.EPERM}),
			folder: "/books/Austen", reason: "operation not permitted",
		},
		{
			name:   "a read-only mount",
			err:    &fs.PathError{Op: "mkdir", Path: "/books/.trash/1", Err: syscall.EROFS},
			folder: "/books/.trash", reason: "read-only file system",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := folderError(lib, root, tt.err)
			if !errors.Is(err, ErrFolderReadOnly) {
				t.Fatalf("%v is not ErrFolderReadOnly", err)
			}
			if got := FolderOf(err); got != tt.folder {
				t.Errorf("folder %q, want %q", got, tt.folder)
			}
			if got := FolderReason(err); got != tt.reason {
				t.Errorf("reason %q, want %q", got, tt.reason)
			}
		})
	}

	for _, err := range []error{
		nil,
		&fs.PathError{Op: "mkdir", Path: "/books/.trash", Err: syscall.ENOSPC},
		&os.LinkError{Op: "rename", Old: "/books/a", New: "/other/a", Err: syscall.EXDEV},
		fs.ErrNotExist,
	} {
		if got := folderError(lib, root, err); got != err {
			t.Errorf("%v became %v", err, got)
		}
	}
}

func TestARefusedRenameNamesTheFolderThatCannotBeWritten(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes into every folder")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "Austen")
	open := filepath.Join(root, ".trash")
	for _, dir := range []string{locked, open} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(locked, "Emma.epub"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	out := os.Rename(filepath.Join(locked, "Emma.epub"), filepath.Join(open, "Emma.epub"))
	refusedOut := folderError(uuid.New(), root, out)
	if got := FolderOf(refusedOut); got != locked {
		t.Errorf("moving out of %s: folder %q", locked, got)
	}
	if got := FolderReason(refusedOut); got != "permission denied" {
		t.Errorf("moving out of %s: reason %q", locked, got)
	}
	if err := os.WriteFile(filepath.Join(open, "Persuasion.epub"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	in := os.Rename(filepath.Join(open, "Persuasion.epub"), filepath.Join(locked, "Persuasion.epub"))
	if got := FolderOf(folderError(uuid.New(), root, in)); got != locked {
		t.Errorf("moving into %s: folder %q", locked, got)
	}
}
