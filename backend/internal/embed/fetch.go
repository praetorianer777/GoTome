package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultSource is where model files come from: Hugging Face, which serves
// every file of a repository at a fixed revision.
const DefaultSource = "https://huggingface.co"

// ErrModelMissing is the error of a model that is not downloaded, or not as
// pinned, when downloading is not allowed.
var ErrModelMissing = errors.New("the embedding model is not downloaded")

// FetchOptions say where model files come from.
type FetchOptions struct {
	// Source is the base URL; DefaultSource when empty.
	Source string
	// Client defaults to one with connection and header timeouts and none
	// on the body, which is 118 MB on whatever line the server has.
	Client *http.Client
	// Offline downloads nothing; a missing model is then ErrModelMissing.
	Offline bool
	Log     *slog.Logger
}

// Fetch makes sure the spec's files lie in root/spec.Dir with the pinned
// size and SHA-256, downloads each that is missing or differs, and returns
// that folder. A file is written beside its place and renamed into it only
// once its hash is right, so an interrupted or corrupted download leaves the
// old file or none, never a wrong one under the right name.
func Fetch(ctx context.Context, root string, spec Spec, opts FetchOptions) (string, error) {
	dir := filepath.Join(root, spec.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	source := opts.Source
	if source == "" {
		source = DefaultSource
	}
	client := opts.Client
	if client == nil {
		client = defaultClient()
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	for _, f := range spec.Files() {
		path := filepath.Join(dir, f.Name)
		ok, err := matches(path, f)
		if err != nil {
			return "", err
		}
		if ok {
			continue
		}
		if opts.Offline {
			return "", fmt.Errorf("%w: %s is missing or not the pinned file, and GOTOME_OFFLINE forbids downloading it", ErrModelMissing, path)
		}
		url := fmt.Sprintf("%s/%s/resolve/%s/onnx/%s", source, spec.Model.Name, spec.Model.Revision, f.Name)
		log.Info("downloading the embedding model", "file", f.Name, "bytes", f.Size, "from", url)
		if err := download(ctx, client, url, path, f); err != nil {
			return "", fmt.Errorf("download %s: %w", f.Name, err)
		}
	}
	return dir, nil
}

func defaultClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}}
}

// matches reports whether the file at path has f's size and hash.
func matches(path string, f File) (bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if info.Size() != f.Size {
		return false, nil
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == f.SHA256, nil
}

func download(ctx context.Context, client *http.Client, url, path string, f File) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", url, resp.Status)
	}
	part, err := os.CreateTemp(filepath.Dir(path), "."+f.Name+".*.part")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			part.Close()
			os.Remove(part.Name())
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(part, h), io.LimitReader(resp.Body, f.Size+1))
	if err != nil {
		return err
	}
	if n != f.Size {
		return fmt.Errorf("got %d bytes, want %d", n, f.Size)
	}
	if sum := hex.EncodeToString(h.Sum(nil)); sum != f.SHA256 {
		return fmt.Errorf("SHA-256 is %s, want %s; nothing kept", sum, f.SHA256)
	}
	if err := part.Close(); err != nil {
		return err
	}
	return os.Rename(part.Name(), path)
}
