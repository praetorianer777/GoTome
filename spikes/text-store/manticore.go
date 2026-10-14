package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Manticore's morphology is a table's, not a field's, so each language is
// a table of its own with the stemmer pg_search and the Tantivy spike use,
// and a distributed table searches the three at once, as the app searches
// its three columns. The text is indexed and not stored.
var manticoreTables = []struct{ name, lang, morphology string }{
	{"c_en", "en", "libstemmer_en"},
	{"c_de", "de", "libstemmer_de"},
	{"c_xx", "xx", "none"},
}

const manticoreAll = "chunks"

func tableOf(lang string) string {
	switch lang {
	case "en", "de":
		return "c_" + lang
	}
	return "c_xx"
}

// Manticore is a client of Manticore's HTTP API, with the standard library
// only, as the app would talk to it.
type Manticore struct {
	base string
	http *http.Client
}

func (m *Manticore) post(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s: %.500s", path, resp.Status, data)
	}
	return data, nil
}

// sqlResult is one statement's answer from /sql?mode=raw.
type sqlResult struct {
	Columns []map[string]any `json:"columns"`
	Data    []map[string]any `json:"data"`
	Total   int              `json:"total"`
	Error   string           `json:"error"`
	Warning string           `json:"warning"`
}

func (m *Manticore) sql(ctx context.Context, stmt string) ([]map[string]any, error) {
	data, err := m.post(ctx, "/sql?mode=raw", "application/x-www-form-urlencoded",
		[]byte("query="+url.QueryEscape(stmt)))
	if err != nil {
		return nil, err
	}
	var res []sqlResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("%.80s: %w: %.300s", stmt, err, data)
	}
	var rows []map[string]any
	for _, r := range res {
		if r.Error != "" {
			return nil, fmt.Errorf("%.80s: %s", stmt, r.Error)
		}
		rows = append(rows, r.Data...)
	}
	return rows, nil
}

func (m *Manticore) exec(ctx context.Context, stmts ...string) error {
	for _, s := range stmts {
		if _, err := m.sql(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func quote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// phase tells bench.sh's sampler of docker stats what is being done.
func setPhase(path, phase string) {
	if path != "" {
		_ = os.WriteFile(path, []byte(phase+"\n"), 0o644)
	}
	fmt.Println("phase", phase)
}

// ManticoreLoad is what loading the chunks measured.
type ManticoreLoad struct {
	Books           int     `json:"books"`
	Chunks          int     `json:"chunks"`
	TextBytes       int64   `json:"textBytes"`
	RTMemLimit      string  `json:"rtMemLimit"`
	Writers         int     `json:"writers"`
	InsertSeconds   float64 `json:"insertSeconds"`
	FlushSeconds    float64 `json:"flushSeconds"`
	OptimizeSeconds float64 `json:"optimizeSeconds"`
}

func waitManticore(ctx context.Context, m *Manticore) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, err := m.sql(ctx, "SHOW STATUS")
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Second)
	}
}

func runManticoreLoad(ctx context.Context, base, chunks, memLimit string, writers int, phase, out string) error {
	m := &Manticore{base: base, http: &http.Client{Timeout: 30 * time.Minute}}
	if err := waitManticore(ctx, m); err != nil {
		return err
	}
	setPhase(phase, "idle-empty")
	time.Sleep(15 * time.Second)

	stmts := []string{"DROP TABLE IF EXISTS " + manticoreAll}
	locals := ""
	for _, t := range manticoreTables {
		stmts = append(stmts, "DROP TABLE IF EXISTS "+t.name, fmt.Sprintf(
			"CREATE TABLE %s (body text indexed, book_id bigint, library_id bigint) morphology='%s' rt_mem_limit='%s'",
			t.name, t.morphology, memLimit))
		locals += fmt.Sprintf(" local='%s'", t.name)
	}
	stmts = append(stmts, "CREATE TABLE "+manticoreAll+" type='distributed'"+locals)
	if err := m.exec(ctx, stmts...); err != nil {
		return err
	}

	res := ManticoreLoad{RTMemLimit: memLimit, Writers: writers}
	setPhase(phase, "indexing")
	start := time.Now()
	batches := make(chan []byte)
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			for b := range batches {
				if err := bulk(ctx, m, b); err != nil {
					errs <- err
					for range batches {
					}
					return
				}
			}
		})
	}
	var buf bytes.Buffer
	n, id := 0, 0
	err := readBooks(chunks, func(b Book) error {
		res.Books++
		for _, c := range b.Chunks {
			id++
			res.Chunks++
			res.TextBytes += int64(len(c.Text))
			if err := writeInsert(&buf, id, b, c); err != nil {
				return err
			}
			n++
			if n == 500 {
				select {
				case batches <- bytes.Clone(buf.Bytes()):
				case err := <-errs:
					return err
				}
				buf.Reset()
				n = 0
			}
		}
		return nil
	})
	if err == nil && n > 0 {
		batches <- buf.Bytes()
	}
	close(batches)
	wg.Wait()
	close(errs)
	if err != nil {
		return err
	}
	if err := <-errs; err != nil {
		return err
	}
	res.InsertSeconds = time.Since(start).Seconds()
	fmt.Printf("%d books, %d chunks inserted in %.0f s\n", res.Books, res.Chunks, res.InsertSeconds)

	setPhase(phase, "flush-optimize")
	start = time.Now()
	for _, t := range manticoreTables {
		if err := m.exec(ctx, "FLUSH RAMCHUNK "+t.name); err != nil {
			return err
		}
	}
	res.FlushSeconds = time.Since(start).Seconds()
	start = time.Now()
	for _, t := range manticoreTables {
		if err := m.exec(ctx, "OPTIMIZE TABLE "+t.name+" OPTION cutoff=1, sync=1"); err != nil {
			return err
		}
	}
	res.OptimizeSeconds = time.Since(start).Seconds()
	fmt.Printf("flushed in %.0f s, optimized to one disk chunk a table in %.0f s\n", res.FlushSeconds, res.OptimizeSeconds)
	setPhase(phase, "idle-loaded")
	time.Sleep(15 * time.Second)
	setPhase(phase, "loaded")
	return writeJSON(out, res)
}

func writeInsert(buf *bytes.Buffer, id int, b Book, c Chunk) error {
	line, err := json.Marshal(map[string]any{"insert": map[string]any{
		"table": tableOf(c.Lang), "id": id,
		"doc": map[string]any{"body": c.Text, "book_id": b.ID, "library_id": b.Library},
	}})
	if err != nil {
		return err
	}
	buf.Write(line)
	buf.WriteByte('\n')
	return nil
}

func bulk(ctx context.Context, m *Manticore, body []byte) error {
	data, err := m.post(ctx, "/bulk", "application/x-ndjson", body)
	if err != nil {
		return err
	}
	var res struct {
		Errors bool   `json:"errors"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return err
	}
	if res.Errors || res.Error != "" {
		return fmt.Errorf("bulk: %.500s", data)
	}
	return nil
}

// ManticoreResult is the whole measurement: the load, the sizes on disk,
// the container's memory by phase, stemming, searches and changes.
type ManticoreResult struct {
	Load           ManticoreLoad                 `json:"load"`
	RestartSeconds float64                       `json:"restartSeconds"`
	Version        string                        `json:"version"`
	Tables         map[string]map[string]any     `json:"tables"`
	Files          map[string]int64              `json:"filesByKind"`
	Memory         map[string]map[string]float64 `json:"memoryMiB"`
	Stemming       []StemCheck                   `json:"stemming"`
	Latency        []Latency                     `json:"latencyMs"`
	Changes        []Latency                     `json:"changesMs"`
	Checks         []string                      `json:"checks"`
}

// StemCheck is the hits of two forms of a query that stemming makes one.
type StemCheck struct {
	Table string `json:"table"`
	A     string `json:"a"`
	B     string `json:"b"`
	HitsA int    `json:"hitsA"`
	HitsB int    `json:"hitsB"`
	Forms string `json:"forms"`
}

type searchHit struct {
	ID     json.Number    `json:"_id"`
	Score  float64        `json:"_score"`
	Source map[string]any `json:"_source"`
}

type searchResponse struct {
	Took int `json:"took"`
	Hits struct {
		Total int         `json:"total"`
		Hits  []searchHit `json:"hits"`
	} `json:"hits"`
}

// search is the app's condition: every word, or the phrase, among the
// libraries, the best first.
func (m *Manticore) search(ctx context.Context, table, text string, phrase bool, libs []int, limit int) (searchResponse, error) {
	var match map[string]any
	if phrase {
		match = map[string]any{"match_phrase": map[string]any{"body": text}}
	} else {
		match = map[string]any{"match": map[string]any{"body": map[string]any{"query": text, "operator": "and"}}}
	}
	must := []any{match}
	if libs != nil {
		must = append(must, map[string]any{"in": map[string]any{"library_id": libs}})
	}
	body, err := json.Marshal(map[string]any{
		"table": table, "limit": limit, "max_matches": max(limit, 1000),
		"_source": []string{"book_id"},
		"query":   map[string]any{"bool": map[string]any{"must": must}},
	})
	if err != nil {
		return searchResponse{}, err
	}
	data, err := m.post(ctx, "/search", "application/json", body)
	if err != nil {
		return searchResponse{}, err
	}
	var res searchResponse
	return res, json.Unmarshal(data, &res)
}

// snippets asks for the snippets of texts handed in, grouped by the table
// whose stemmer made the hit.
func (m *Manticore) snippets(ctx context.Context, query string, phrase bool, texts []handedIn) (int, error) {
	if phrase {
		query = `"` + query + `"`
	}
	byTable := map[string][]string{}
	for _, t := range texts {
		byTable[t.table] = append(byTable[t.table], quote(t.text))
	}
	n := 0
	for _, t := range manticoreTables {
		docs := byTable[t.name]
		if len(docs) == 0 {
			continue
		}
		rows, err := m.sql(ctx, fmt.Sprintf(
			"CALL SNIPPETS((%s), '%s', %s, '\x02' AS before_match, '\x03' AS after_match, %d AS limit)",
			strings.Join(docs, ","), t.name, quote(query), snippetChars))
		if err != nil {
			return n, err
		}
		for _, r := range rows {
			if s, _ := r["snippet"].(string); strings.Contains(s, "\x02") {
				n++
			}
		}
	}
	return n, nil
}

type handedIn struct{ table, text string }

// readTexts reads the chunks in want back from what extract wrote, numbered
// as they were inserted: the text the app would hand in from elsewhere.
func readTexts(chunks string, want map[int]bool) (map[int]handedIn, map[int][]int, error) {
	texts := map[int]handedIn{}
	books := map[int][]int{}
	id := 0
	err := readBooks(chunks, func(b Book) error {
		for _, c := range b.Chunks {
			id++
			if want[id] || want[-b.ID] {
				texts[id] = handedIn{tableOf(c.Lang), c.Text}
				books[b.ID] = append(books[b.ID], id)
			}
		}
		return nil
	})
	return texts, books, err
}

func percentiles(l *Latency, times []float64) {
	slices.Sort(times)
	l.P50 = times[len(times)/2]
	l.P95 = times[(len(times)*95+99)/100-1]
	l.Max = times[len(times)-1]
}

func runManticoreSearch(ctx context.Context, restart float64, base, chunks, queries, loadPath, filesPath, samples, phase string, runs int, windows []int, out string) error {
	m := &Manticore{base: base, http: &http.Client{Timeout: 5 * time.Minute}}
	if err := waitManticore(ctx, m); err != nil {
		return err
	}
	res := ManticoreResult{RestartSeconds: restart}
	if data, err := os.ReadFile(loadPath); err != nil {
		return err
	} else if err := json.Unmarshal(data, &res.Load); err != nil {
		return err
	}
	if rows, err := m.sql(ctx, "SHOW STATUS LIKE 'version'"); err == nil && len(rows) > 0 {
		res.Version = fmt.Sprint(rows[0]["Value"])
	}
	setPhase(phase, "idle-restarted")
	time.Sleep(15 * time.Second)

	res.Tables = map[string]map[string]any{}
	for _, t := range manticoreTables {
		rows, err := m.sql(ctx, "SHOW TABLE "+t.name+" STATUS")
		if err != nil {
			return err
		}
		st := map[string]any{}
		for _, r := range rows {
			k := fmt.Sprint(r["Variable_name"])
			if strings.HasSuffix(k, "_bytes") || k == "indexed_documents" || k == "disk_chunks" || k == "ram_chunk" || k == "mem_limit" || k == "killed_documents" {
				st[k] = r["Value"]
			}
		}
		res.Tables[t.name] = st
	}
	if filesPath != "" {
		res.Files = filesByKind(filesPath)
	}

	setPhase(phase, "stemming")
	for _, c := range []struct{ table, a, b string }{
		{"c_en", "running", "runs"},
		{"c_en", `"he was running"`, `"he was runs"`},
		{"c_de", "nächte", "nacht"},
		{"c_de", `"zum ersten male"`, `"zum ersten mal"`},
	} {
		sc := StemCheck{Table: c.table, A: c.a, B: c.b}
		for i, q := range []string{c.a, c.b} {
			phrase := strings.HasPrefix(q, `"`)
			r, err := m.search(ctx, c.table, strings.Trim(q, `"`), phrase, nil, 1)
			if err != nil {
				return err
			}
			if i == 0 {
				sc.HitsA = r.Hits.Total
			} else {
				sc.HitsB = r.Hits.Total
			}
		}
		rows, err := m.sql(ctx, fmt.Sprintf("CALL KEYWORDS(%s, '%s')", quote(strings.Trim(c.a+" "+c.b, `"`)), c.table))
		if err != nil {
			return err
		}
		var forms []string
		for _, r := range rows {
			forms = append(forms, fmt.Sprintf("%v→%v", r["tokenized"], r["normalized"]))
		}
		sc.Forms = strings.Join(forms, " ")
		fmt.Printf("  %s %q %d, %q %d: %s\n", sc.Table, sc.A, sc.HitsA, sc.B, sc.HitsB, sc.Forms)
		res.Stemming = append(res.Stemming, sc)
	}

	qs, err := loadQueries(queries)
	if err != nil {
		return err
	}
	setPhase(phase, "searching")
	for _, window := range windows {
		for _, q := range qs {
			text, phrase := q.Words, false
			if q.Phrase != "" {
				text, phrase = q.Phrase, true
			}
			libs := []int{0, 1}
			if q.Library {
				libs = libs[:1]
			}
			var texts map[int]handedIn
			if q.Snippets {
				r, err := m.search(ctx, manticoreAll, text, phrase, libs, window)
				if err != nil {
					return err
				}
				want := map[int]bool{}
				for _, h := range r.Hits.Hits[:min(len(r.Hits.Hits), snippetHits)] {
					id, _ := strconv.Atoi(string(h.ID))
					want[id] = true
				}
				if texts, _, err = readTexts(chunks, want); err != nil {
					return err
				}
			}
			l := Latency{Kind: q.Kind, Query: text, Top: window}
			var times, took []float64
			for i := range runs + 1 {
				start := time.Now()
				r, err := m.search(ctx, manticoreAll, text, phrase, libs, window)
				if err != nil {
					return fmt.Errorf("%s %q: %w", q.Kind, text, err)
				}
				l.Hits, l.Total = len(r.Hits.Hits), r.Hits.Total
				if q.Snippets {
					var hand []handedIn
					for _, h := range r.Hits.Hits[:min(len(r.Hits.Hits), snippetHits)] {
						id, _ := strconv.Atoi(string(h.ID))
						if t, ok := texts[id]; ok {
							hand = append(hand, t)
						}
					}
					if l.Snippets, err = m.snippets(ctx, text, phrase, hand); err != nil {
						return err
					}
				}
				if i > 0 {
					times = append(times, float64(time.Since(start).Microseconds())/1000)
					took = append(took, float64(r.Took))
				}
			}
			percentiles(&l, times)
			slices.Sort(took)
			l.Took = took[len(took)/2]
			fmt.Printf("  top %4d %-8s %-24q %5d hits (%6d)  p50 %7.1f ms  p95 %7.1f ms  server %4.0f ms\n",
				window, l.Kind, l.Query, l.Hits, l.Total, l.P50, l.P95, l.Took)
			res.Latency = append(res.Latency, l)
		}
	}

	setPhase(phase, "changes")
	if err := measureChanges(ctx, m, chunks, &res); err != nil {
		return err
	}
	setPhase(phase, "done")
	if samples != "" {
		res.Memory = memoryByPhase(samples)
	}
	return writeJSON(out, res)
}

// measureChanges times what the app does to a book's chunks: a library
// move as an attribute update, deleting them by book and by ID, and
// writing them again, and checks each against a count.
func measureChanges(ctx context.Context, m *Manticore, chunks string, res *ManticoreResult) error {
	var picked []int
	for id := 50; id <= res.Load.Books && len(picked) < 20; id += 97 {
		picked = append(picked, id)
	}
	want := map[int]bool{}
	for _, b := range picked {
		want[-b] = true
	}
	texts, books, err := readTexts(chunks, want)
	if err != nil {
		return err
	}
	count := func(where string) (int, error) {
		rows, err := m.sql(ctx, "SELECT COUNT(*) AS n FROM "+manticoreAll+" WHERE "+where)
		if err != nil || len(rows) == 0 {
			return 0, err
		}
		n, err := strconv.Atoi(fmt.Sprint(rows[0]["n"]))
		return n, err
	}
	everyTable := func(format string, args ...any) error {
		for _, t := range manticoreTables {
			if err := m.exec(ctx, fmt.Sprintf(format, append([]any{t.name}, args...)...)); err != nil {
				return err
			}
		}
		return nil
	}
	insert := func(book int) error {
		var buf bytes.Buffer
		for _, id := range books[book] {
			t := texts[id]
			lang := strings.TrimPrefix(t.table, "c_")
			if err := writeInsert(&buf, id, Book{ID: book, Library: book % Libraries}, Chunk{Lang: lang, Text: t.text}); err != nil {
				return err
			}
		}
		return bulk(ctx, m, buf.Bytes())
	}
	steps := []struct {
		name string
		do   func(book int) error
		left func(book int) (int, bool)
	}{
		{"move library (UPDATE library_id)", func(b int) error {
			return everyTable("UPDATE %s SET library_id=%d WHERE book_id=%d", 7, b)
		}, func(b int) (int, bool) { return len(books[b]), true }},
		{"delete by book (DELETE WHERE book_id)", func(b int) error {
			return everyTable("DELETE FROM %s WHERE book_id=%d", b)
		}, func(int) (int, bool) { return 0, false }},
		{"insert a book's chunks (/bulk)", insert, func(b int) (int, bool) { return len(books[b]), false }},
		{"delete by chunk ID (DELETE WHERE id IN)", func(b int) error {
			ids := make([]string, len(books[b]))
			for i, id := range books[b] {
				ids[i] = strconv.Itoa(id)
			}
			return everyTable("DELETE FROM %s WHERE id IN (%s)", strings.Join(ids, ","))
		}, func(int) (int, bool) { return 0, false }},
		{"insert again (/bulk)", insert, func(b int) (int, bool) { return len(books[b]), false }},
	}
	for _, s := range steps {
		l := Latency{Kind: "change", Query: s.name}
		var times []float64
		for _, b := range picked {
			start := time.Now()
			if err := s.do(b); err != nil {
				return fmt.Errorf("%s: %w", s.name, err)
			}
			times = append(times, float64(time.Since(start).Microseconds())/1000)
			n, moved := s.left(b)
			where := fmt.Sprintf("book_id=%d", b)
			if moved {
				where += " AND library_id=7"
			}
			got, err := count(where)
			if err != nil {
				return err
			}
			if got != n {
				res.Checks = append(res.Checks, fmt.Sprintf("%s: book %d has %d chunks, want %d", s.name, b, got, n))
			}
			l.Hits += len(books[b])
		}
		percentiles(&l, times)
		fmt.Printf("  %-42s %d books, %d chunks  p50 %6.1f ms  p95 %6.1f ms\n", s.name, len(picked), l.Hits, l.P50, l.P95)
		res.Changes = append(res.Changes, l)
	}
	n, err := count("library_id=7")
	if err != nil {
		return err
	}
	res.Checks = append(res.Checks, fmt.Sprintf("after the changes %d chunks remain in the moved library (0 expected: the books were written again in their own)", n))
	return nil
}

// filesByKind sums the sizes in a "size name" listing of the data
// directory by file extension.
func filesByKind(path string) map[string]int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]int64{}
	for line := range strings.Lines(string(data)) {
		size, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		n, err := strconv.ParseInt(size, 10, 64)
		if !ok || err != nil {
			continue
		}
		ext := "other"
		if i := strings.LastIndexByte(name, '.'); i >= 0 {
			ext = name[i+1:]
		}
		out[ext] += n
		out["total"] += n
	}
	return out
}

// memoryByPhase reads bench.sh's samples, "phase usage" per line with the
// usage as docker stats prints it, and gives each phase's median and peak.
func memoryByPhase(path string) map[string]map[string]float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	by := map[string][]float64{}
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if mib, err := parseMiB(f[1]); err == nil {
			by[f[0]] = append(by[f[0]], mib)
		}
	}
	out := map[string]map[string]float64{}
	for p, v := range by {
		slices.Sort(v)
		out[p] = map[string]float64{"median": v[len(v)/2], "peak": v[len(v)-1], "samples": float64(len(v))}
	}
	return out
}

func parseMiB(s string) (float64, error) {
	units := []struct {
		suffix string
		mib    float64
	}{{"GiB", 1024}, {"MiB", 1}, {"KiB", 1.0 / 1024}, {"GB", 1e9 / (1 << 20)}, {"MB", 1e6 / (1 << 20)}, {"kB", 1e3 / (1 << 20)}, {"B", 1.0 / (1 << 20)}}
	for _, u := range units {
		if v, ok := strings.CutSuffix(s, u.suffix); ok {
			n, err := strconv.ParseFloat(v, 64)
			return n * u.mib, err
		}
	}
	return 0, errors.New("no unit in " + s)
}
