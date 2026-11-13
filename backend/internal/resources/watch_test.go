package resources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/embed"
)

type logBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logBuffer) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Split(strings.TrimSpace(l.b.String()), "\n")
}

func (l *logBuffer) logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, nil))
}

func TestWatchLogsAFailureOnceAndAgainWhenItChanges(t *testing.T) {
	var mu sync.Mutex
	calls, message := 0, "first"
	s := &Service{every: time.Millisecond}
	s.memory = func() (int64, int64) { return 0, 0 }
	s.Embedding = func(context.Context) (embed.Spec, bool, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return embed.Spec{}, false, errors.New(message)
	}
	var out logBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Watch(ctx, out.logger()); close(done) }()

	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("no %s; log: %v", what, out.lines())
			}
		}
	}
	waitFor("repeated checks", func() bool { mu.Lock(); defer mu.Unlock(); return calls >= 5 })
	if got := out.lines(); len(got) != 1 || !strings.Contains(got[0], "could not check") || !strings.Contains(got[0], "first") {
		t.Fatalf("the same failure logged again: %v", got)
	}
	mu.Lock()
	message = "second"
	mu.Unlock()
	waitFor("the new failure", func() bool { return len(out.lines()) == 2 })
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return when its context ended")
	}
	if got := out.lines(); len(got) != 2 || !strings.Contains(got[1], "second") {
		t.Errorf("%v", got)
	}
}

func TestWatchStopsWhenItsContextIsEnded(t *testing.T) {
	s := &Service{}
	s.Embedding = func(context.Context) (embed.Spec, bool, error) { return embed.Spec{}, false, errors.New("x") }
	s.memory = func() (int64, int64) { return 0, 0 }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out logBuffer
	done := make(chan struct{})
	go func() { s.Watch(ctx, out.logger()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch kept running")
	}
	if got := strings.TrimSpace(strings.Join(out.lines(), "")); got != "" {
		t.Errorf("logged after its context ended: %q", got)
	}
}

func TestLogHealthSaysOneLineWhenAllFitsAndOneForEachWarning(t *testing.T) {
	var out logBuffer
	logHealth(out.logger(), defaults(8*gb))
	got := out.lines()
	if len(got) != 1 || !strings.Contains(got[0], "level=INFO") || !strings.Contains(got[0], "has room") {
		t.Fatalf("%v", got)
	}

	h := defaults(8 * gb)
	h.Disk.FreeBytes = 1 * gb
	h.Warnings = []Warning{
		{Kind: KindDisk},
		{Kind: KindParallel, Commands: []string{"ALTER SYSTEM SET max_parallel_workers = 2;", "SELECT 1;"}},
	}
	var warned logBuffer
	logHealth(warned.logger(), h)
	got = warned.lines()
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
	for i, w := range h.Warnings {
		if !strings.Contains(got[i], "level=WARN") || !strings.Contains(got[i], "kind="+w.Kind) || !strings.Contains(got[i], messages[w.Kind][:30]) {
			t.Errorf("line %d: %s", i, got[i])
		}
	}
	if strings.Contains(got[0], "commands=") || !strings.Contains(got[1], "max_parallel_workers = 2; SELECT 1;") {
		t.Errorf("commands: %v", got)
	}
}

func TestEveryKindOfWarningHasAMessage(t *testing.T) {
	for _, k := range []string{KindMemory, KindEmbedding, KindParallel, KindDisk, KindWatches} {
		if messages[k] == "" {
			t.Errorf("no message for %s", k)
		}
	}
}

func TestHealthKeyTellsStatesApart(t *testing.T) {
	a := Health{Warnings: []Warning{{Kind: KindDisk}, {Kind: KindMemory, Commands: []string{"x", "y"}}}}
	b := Health{Warnings: []Warning{{Kind: KindDisk}, {Kind: KindMemory, Commands: []string{"x"}}}}
	if got := a.key(); got != "disk:|memory:x y" {
		t.Errorf("%q", got)
	}
	if a.key() == b.key() || a.key() != a.key() {
		t.Error("other commands are the same state")
	}
	if (Health{}).key() != "" {
		t.Error("no warnings is not the empty key")
	}
}

func TestFolderSizeAddsUpFilesInSubfoldersAndIgnoresWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, n := range map[string]int{"one": 10, "a/two": 200, "a/b/three": 3000} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := folderSize(dir); got != 3210 {
		t.Errorf("%d", got)
	}
	if got := folderSize(filepath.Join(dir, "nope")); got != 0 {
		t.Errorf("a missing folder is %d", got)
	}
	if got := folderSize(filepath.Join(dir, "one")); got != 10 {
		t.Errorf("a file is %d", got)
	}
}
