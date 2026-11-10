package ingest

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type requests struct {
	mu  sync.Mutex
	ids []uuid.UUID
}

func (r *requests) add(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	return nil
}

func (r *requests) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ids)
}

func startWatcher(t *testing.T, root string, polling bool) (*Watcher, *requests) {
	t.Helper()
	id := uuid.New()
	got := &requests{}
	w := &Watcher{
		Targets: func(context.Context) ([]WatchTarget, error) {
			return []WatchTarget{{ID: id, Root: root}}, nil
		},
		Request:   got.add,
		Quiet:     300 * time.Millisecond,
		MaxWait:   5 * time.Second,
		PollEvery: 100 * time.Millisecond,
		SyncEvery: time.Hour,
		Tick:      20 * time.Millisecond,
		Polling:   polling,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	waitFor(t, func() bool { s := w.Status(); return s.Notified+s.Polled == 1 })
	return w, got
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quiet(t *testing.T, got *requests, want int) {
	t.Helper()
	time.Sleep(1200 * time.Millisecond)
	if n := got.count(); n != want {
		t.Fatalf("%d scans requested, want %d", n, want)
	}
}

func TestWatcherScansOnceAfterABurstOfNewFiles(t *testing.T) {
	for _, polling := range []bool{false, true} {
		root := t.TempDir()
		touch(t, filepath.Join(root, "Author", "a.epub"))
		w, got := startWatcher(t, root, polling)
		if !polling && w.Status().Notified != 1 {
			t.Skip("inotify is not available here")
		}
		for _, name := range []string{"b.epub", "c.pdf", "d.mp3"} {
			touch(t, filepath.Join(root, "Author", "Series", name))
			time.Sleep(50 * time.Millisecond)
		}
		waitFor(t, func() bool { return got.count() == 1 })
		quiet(t, got, 1)
	}
}

func TestWatcherLeavesDotFoldersAndOtherFilesAlone(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a.epub"))
	_, got := startWatcher(t, root, false)
	touch(t, filepath.Join(root, ".uploads", "part.epub"))
	touch(t, filepath.Join(root, ".trash", "1", "x.epub"))
	touch(t, filepath.Join(root, "@eaDir", "x.epub"))
	touch(t, filepath.Join(root, "notes.txt"))
	touch(t, filepath.Join(root, ".hidden.epub"))
	quiet(t, got, 0)
}

func TestWatcherCountsOneWatchPerFolder(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		for j := 0; j < 20; j++ {
			touch(t, filepath.Join(root, "d"+string(rune('a'+i)), "f"+string(rune('a'+j))+".epub"))
		}
	}
	touch(t, filepath.Join(root, ".trash", "x.epub"))
	w, _ := startWatcher(t, root, false)
	if s := w.Status(); s.Notified == 1 && s.Watches != 4 {
		t.Fatalf("%d watches, want 4 (root and three folders)", s.Watches)
	}
}

func TestPollingSeesAFolderAppearAndAFileGo(t *testing.T) {
	root := t.TempDir()
	touch(t, filepath.Join(root, "a", "a.epub"))
	_, got := startWatcher(t, root, true)
	if err := os.Remove(filepath.Join(root, "a", "a.epub")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return got.count() == 1 })
}

func TestWatcherPollsALibraryTheKernelHasNoWatchesFor(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b", "c", "d"} {
		touch(t, filepath.Join(root, d, "x.epub"))
	}
	id := uuid.New()
	got := &requests{}
	var taken int
	w := &Watcher{
		Targets: func(context.Context) ([]WatchTarget, error) {
			return []WatchTarget{{ID: id, Root: root}}, nil
		},
		Request: got.add, Quiet: 300 * time.Millisecond, PollEvery: 100 * time.Millisecond,
		SyncEvery: time.Hour, Tick: 20 * time.Millisecond,
		addWatch: func(fd int, path string, mask uint32) (int, error) {
			if taken == 3 {
				return -1, unix.ENOSPC
			}
			taken++
			return unix.InotifyAddWatch(fd, path, mask)
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	waitFor(t, func() bool { return w.Status().LimitReached })
	s := w.Status()
	if s.Watches != 0 || s.Polled != 1 || s.Notified != 0 {
		t.Fatalf("status %+v: want no watches kept and the library polled", s)
	}
	if err := os.Remove(filepath.Join(root, "d", "x.epub")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return got.count() == 1 })
}
