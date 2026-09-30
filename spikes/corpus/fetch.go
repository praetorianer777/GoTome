package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// HarvestURL is the one entry point Project Gutenberg offers to robots; the
	// site itself is for human readers and blocks automated access.
	// https://www.gutenberg.org/policy/robot_access.html
	HarvestURL = "https://www.gutenberg.org/robot/harvest"
	// DefaultDelay is the pause that policy asks for between requests.
	DefaultDelay = 2 * time.Second
	// DefaultLanguages makes about 2,000 books, mostly English and German, the
	// two languages the search spike has to stem.
	DefaultLanguages = "en:1200,de:600,fr:200"

	userAgent = "GOtome-corpus/0.1 (+https://github.com/praetorianer777/GoTome)"
	// EPUBs without images are a few hundred kilobytes; anything near this is
	// not a book the benchmark wants.
	maxEPUBBytes = 64 << 20
	maxPageBytes = 4 << 20
)

// Quota is how many books of one language the corpus should hold.
type Quota struct {
	Lang  string
	Count int
}

var langCode = regexp.MustCompile(`^[a-z]{2,3}$`)

// ParseLanguages reads "en:1200,de:600" into quotas.
func ParseLanguages(s string) ([]Quota, error) {
	var quotas []Quota
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		lang, count, ok := strings.Cut(strings.TrimSpace(part), ":")
		n, err := strconv.Atoi(count)
		if !ok || err != nil || n <= 0 || !langCode.MatchString(lang) {
			return nil, fmt.Errorf("language %q is not code:count, such as de:600", part)
		}
		if seen[lang] {
			return nil, fmt.Errorf("language %q is listed twice", lang)
		}
		seen[lang] = true
		quotas = append(quotas, Quota{Lang: lang, Count: n})
	}
	return quotas, nil
}

// Fetcher downloads EPUBs listed by the harvest endpoint.
type Fetcher struct {
	Dir     string
	Harvest string
	Delay   time.Duration
	Client  *http.Client
	Log     io.Writer
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 2 * time.Minute}
}

// Fetch fills Dir/<lang>/ up to each quota. Files already there count towards
// it, so an interrupted run picks up where it stopped.
func (f *Fetcher) Fetch(ctx context.Context, quotas []Quota) error {
	for _, q := range quotas {
		if err := f.fetchLanguage(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", q.Lang, err)
		}
	}
	return nil
}

func (f *Fetcher) fetchLanguage(ctx context.Context, q Quota) error {
	dir := filepath.Join(f.Dir, q.Lang)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	existing, err := filepath.Glob(filepath.Join(dir, "*.epub"))
	if err != nil {
		return err
	}
	have := len(existing)

	page := f.Harvest + "?" + url.Values{
		"filetypes[]": {"epub.noimages"},
		"langs[]":     {q.Lang},
	}.Encode()

	for have < q.Count && page != "" {
		body, err := f.get(ctx, page, maxPageBytes)
		if err != nil {
			return fmt.Errorf("harvest page: %w", err)
		}
		files, next, err := parseHarvest(page, body)
		if err != nil {
			return err
		}
		for _, file := range files {
			if have >= q.Count {
				break
			}
			dst := filepath.Join(dir, path.Base(file.Path))
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if err := f.download(ctx, file.String(), dst); err != nil {
				// One unreadable book must not end a run of two thousand.
				fmt.Fprintf(f.Log, "skipped %s: %v\n", file, err)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
			have++
			if have%50 == 0 || have == q.Count {
				fmt.Fprintf(f.Log, "%s: %d/%d\n", q.Lang, have, q.Count)
			}
		}
		page = next
	}
	if have < q.Count {
		fmt.Fprintf(f.Log, "%s: the harvest ended at %d of %d books\n", q.Lang, have, q.Count)
	}
	return nil
}

var href = regexp.MustCompile(`(?i)href="([^"]+)"`)

// parseHarvest splits a harvest page into the EPUBs it lists and the page that
// follows it, which is the one link back to the harvest endpoint itself.
func parseHarvest(pageURL string, body []byte) (files []*url.URL, next string, err error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, "", err
	}
	for _, m := range href.FindAllSubmatch(body, -1) {
		ref, err := base.Parse(htmlUnescape(string(m[1])))
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(ref.Path, ".epub"):
			files = append(files, ref)
		case ref.Host == base.Host && ref.Path == base.Path && ref.Query().Has("offset"):
			next = ref.String()
		}
	}
	return files, next, nil
}

func htmlUnescape(s string) string { return strings.ReplaceAll(s, "&amp;", "&") }

func (f *Fetcher) download(ctx context.Context, src, dst string) error {
	body, err := f.get(ctx, src, maxEPUBBytes)
	if err != nil {
		return err
	}
	if err := checkEPUB(body); err != nil {
		return err
	}
	// Written under another name first, so a run killed mid-write leaves
	// nothing that the next run would count as a finished book.
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// get waits out the delay first, so every request is spaced from the one
// before it whatever that one was.
func (f *Fetcher) get(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(f.Delay):
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return body, nil
}

// checkEPUB accepts what has the shape of an EPUB: a zip with a container
// file. A mirror's error page saved as .epub would poison every benchmark.
func checkEPUB(body []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("not a zip: %w", err)
	}
	for _, file := range zr.File {
		if file.Name == containerPath {
			return nil
		}
	}
	return errors.New("no " + containerPath)
}
