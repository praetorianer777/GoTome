package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func testSpec(files map[string]string) Spec {
	file := func(name string) File {
		sum := sha256.Sum256([]byte(files[name]))
		return File{Name: name, Size: int64(len(files[name])), SHA256: hex.EncodeToString(sum[:])}
	}
	return Spec{
		Model:     Model{Name: "org/model", Revision: "abc"},
		Dir:       "model",
		Weights:   file("model.onnx"),
		Tokenizer: file("tokenizer.json"),
	}
}

var quiet = slog.New(slog.DiscardHandler)

type modelServer struct {
	*httptest.Server
	files    map[string]string
	requests atomic.Int32
}

func newModelServer(t *testing.T, files map[string]string) *modelServer {
	s := &modelServer{files: files}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		name, ok := strings.CutPrefix(r.URL.Path, "/org/model/resolve/abc/onnx/")
		body, found := s.files[name]
		if !ok || !found {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s
}

func TestFetchDownloadsOnceAndKeepsWhatIsRight(t *testing.T) {
	files := map[string]string{"model.onnx": "weights", "tokenizer.json": `{"model":{}}`}
	spec := testSpec(files)
	srv := newModelServer(t, files)
	root := t.TempDir()
	opts := FetchOptions{Source: srv.URL, Client: srv.Client(), Log: quiet}

	dir, err := Fetch(context.Background(), root, spec, opts)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(root, "model") {
		t.Errorf("dir = %s", dir)
	}
	for name, body := range files {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != body {
			t.Errorf("%s holds %q, want %q", name, b, body)
		}
	}
	if n := srv.requests.Load(); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
	if _, err := Fetch(context.Background(), root, spec, opts); err != nil {
		t.Fatal(err)
	}
	if n := srv.requests.Load(); n != 2 {
		t.Errorf("a second fetch made %d requests in all, want none more", n)
	}
}

func TestFetchReplacesACorruptedFile(t *testing.T) {
	files := map[string]string{"model.onnx": "weights", "tokenizer.json": "tok"}
	spec := testSpec(files)
	srv := newModelServer(t, files)
	root := t.TempDir()
	opts := FetchOptions{Source: srv.URL, Client: srv.Client(), Log: quiet}
	dir, err := Fetch(context.Background(), root, spec, opts)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, other bytes: only the hash tells.
	if err := os.WriteFile(filepath.Join(dir, "model.onnx"), []byte("weighTs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Fetch(context.Background(), root, spec, opts); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "model.onnx")); string(b) != "weights" {
		t.Errorf("model.onnx holds %q after a fetch, want it downloaded again", b)
	}
	if n := srv.requests.Load(); n != 3 {
		t.Errorf("%d requests, want 3: both files, then the corrupted one", n)
	}
}

func TestFetchOfflineDownloadsNothing(t *testing.T) {
	files := map[string]string{"model.onnx": "weights", "tokenizer.json": "tok"}
	srv := newModelServer(t, files)
	_, err := Fetch(context.Background(), t.TempDir(), testSpec(files),
		FetchOptions{Source: srv.URL, Client: srv.Client(), Offline: true})
	if !errors.Is(err, ErrModelMissing) {
		t.Errorf("err = %v, want ErrModelMissing", err)
	}
	if n := srv.requests.Load(); n != 0 {
		t.Errorf("%d requests while offline", n)
	}
}

func TestFetchKeepsNothingOfAWrongDownload(t *testing.T) {
	files := map[string]string{"model.onnx": "weights", "tokenizer.json": "tok"}
	spec := testSpec(files)
	srv := newModelServer(t, map[string]string{"model.onnx": "tampers", "tokenizer.json": "tok"})
	root := t.TempDir()
	_, err := Fetch(context.Background(), root, spec, FetchOptions{Source: srv.URL, Client: srv.Client(), Log: quiet})
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "model"))
	if len(entries) != 0 {
		t.Errorf("left behind: %v", entries)
	}
}
