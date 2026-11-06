package passages

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Meta is what the store's vectors are: the model that made them and the
// centre their signs are taken around, fixed when the store is begun.
type Meta struct {
	Model   string `json:"model"`
	Version string `json:"version"`
	Dim     int    `json:"dim"`
	// Centre is the mean of the first passages stored. e5's vectors share
	// a large common part, so the plain signs of two passages mostly agree
	// whatever they say; around the mean they tell passages apart
	// (docs/decisions/passage-vectors.md).
	Centre []float32 `json:"centre"`
	// Libraries are the libraries the records name, by their place here.
	Libraries   []uuid.UUID `json:"libraries"`
	CompactedAt time.Time   `json:"compactedAt"`
	// Generation names the data files the records are in. Begin and
	// Compact write files of a new generation and switch to them by
	// replacing meta.json, one rename, so a crash leaves either set whole.
	// Generation 0 is the files of a store written before there were
	// generations.
	Generation int `json:"generation,omitempty"`
}

// Record is one passage to store.
type Record struct {
	Chunk   int64
	Library uuid.UUID
	Vector  []float32
}

// Hit is a passage a search found, with the cosine of its int8 vector.
type Hit struct {
	Chunk int64
	Score float64
}

// Store keeps every passage's vector in two files of fixed-size records,
// the record's place the same in both: signs (the chunk, its library and
// a bit per dimension) are read whole by every search, vectors (the int8
// vector and its scale) only for the passages the signs rank best. It
// holds no text and is derived: the chunks are in Postgres, and what it
// finds is checked against them.
type Store struct {
	dir string
	mu  sync.RWMutex
	// meta is nil for a store not begun, and the files with it.
	meta  *Meta
	signs *os.File
	vecs  *os.File
	n     int
	// last is the highest generation taken, so that a Begin and a Compact
	// never write the same files.
	last        int
	startedOver bool
}

const (
	metaFile  = "meta.json"
	signsFile = "signs"
	vecsFile  = "vectors"
)

// dataFiles are the names of a generation's signs and vectors.
func dataFiles(gen int) (string, string) {
	if gen == 0 {
		return signsFile, vecsFile
	}
	return fmt.Sprintf("%s.%d", signsFile, gen), fmt.Sprintf("%s.%d", vecsFile, gen)
}

// OpenStore opens the store in dir, made if missing. A record half
// written when the process stopped is cut off. A store whose data files
// are not the set its meta.json names is started over, as is one whose
// compaction was cut off between its renames before there were
// generations: its records would pair one passage's signs with another's
// vector. Files of no generation in use are removed.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	s := &Store{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, metaFile))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		var m Meta
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("passage store: %w", err)
		}
		s.last = m.Generation
		whole, err := s.whole(&m)
		if err != nil {
			return nil, err
		}
		if whole {
			s.meta = &m
		} else {
			s.startedOver = true
		}
	}
	if err := s.sweep(); err != nil {
		return nil, err
	}
	if s.meta == nil {
		return s, nil
	}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

// StartedOver says OpenStore found the files not of one set and began
// the store again: its passages are to be embedded anew.
func (s *Store) StartedOver() bool { return s.startedOver }

// whole says the data files m names are there. Of a store before
// generations, a vectors.new without its signs.new is a compaction that
// stopped between the renames of the two files.
func (s *Store) whole(m *Meta) (bool, error) {
	sf, vf := dataFiles(m.Generation)
	for _, f := range []string{sf, vf} {
		if _, err := os.Stat(filepath.Join(s.dir, f)); errors.Is(err, os.ErrNotExist) {
			return false, nil
		} else if err != nil {
			return false, err
		}
	}
	if m.Generation > 0 {
		return true, nil
	}
	_, err := os.Stat(filepath.Join(s.dir, signsFile+".new"))
	signsLeft := err == nil
	_, err = os.Stat(filepath.Join(s.dir, vecsFile+".new"))
	vecsLeft := err == nil
	return signsLeft || !vecsLeft, nil
}

// sweep removes the data files of other generations, and meta.json with
// them when the store is not begun.
func (s *Store) sweep() error {
	var sf, vf string
	if s.meta != nil {
		sf, vf = dataFiles(s.meta.Generation)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		name := e.Name()
		ours := name == metaFile && s.meta == nil || name == metaFile+".new" ||
			strings.HasPrefix(name, signsFile) && name != sf || strings.HasPrefix(name, vecsFile) && name != vf
		if ours && !e.IsDir() {
			errs = append(errs, os.Remove(filepath.Join(s.dir, name)))
		}
	}
	return errors.Join(errs...)
}

// open opens the data files of s.meta's generation and cuts off a record
// half written.
func (s *Store) open() error {
	sf, vf := dataFiles(s.meta.Generation)
	var err error
	if s.signs, err = os.OpenFile(filepath.Join(s.dir, sf), os.O_RDWR, 0); err != nil {
		return err
	}
	if s.vecs, err = os.OpenFile(filepath.Join(s.dir, vf), os.O_RDWR, 0); err != nil {
		s.signs.Close()
		return err
	}
	ss, err := s.signs.Stat()
	if err != nil {
		return err
	}
	vs, err := s.vecs.Stat()
	if err != nil {
		return err
	}
	s.n = int(min(ss.Size()/int64(signsSize(s.meta.Dim)), vs.Size()/int64(vecSize(s.meta.Dim))))
	if err := s.signs.Truncate(int64(s.n * signsSize(s.meta.Dim))); err != nil {
		return err
	}
	return s.vecs.Truncate(int64(s.n * vecSize(s.meta.Dim)))
}

// signsSize is a record of the signs file: the chunk, the library's place
// and the signs.
func signsSize(dim int) int { return 8 + 2 + (dim+7)/8 }

// vecSize is a record of the vectors file: the scale and a byte a
// dimension.
func vecSize(dim int) int { return 4 + dim }

// Close closes the files.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeFiles()
}

// closeFiles closes the data files open; the caller holds the lock.
func (s *Store) closeFiles() error {
	var errs []error
	for _, f := range []*os.File{s.signs, s.vecs} {
		if f != nil {
			errs = append(errs, f.Close())
		}
	}
	s.signs, s.vecs, s.n = nil, nil, 0
	return errors.Join(errs...)
}

// Meta is a copy of what the store holds, nil before it is begun.
func (s *Store) Meta() *Meta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.meta == nil {
		return nil
	}
	m := *s.meta
	m.Libraries = slices.Clone(m.Libraries)
	return &m
}

// Len is how many records the store holds, those of chunks gone since
// among them until it is compacted.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.n
}

// Size is the store's bytes on disk.
func (s *Store) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.meta == nil {
		return 0
	}
	return int64(s.n * (signsSize(s.meta.Dim) + vecSize(s.meta.Dim)))
}

// Begin empties the store for vectors of another model, around the given
// centre.
func (s *Store) Begin(model, version string, centre []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	gen := s.last
	sf, vf := dataFiles(gen)
	for _, f := range []string{sf, vf} {
		if err := writeFile(filepath.Join(s.dir, f), nil); err != nil {
			return errors.Join(err, s.removeGeneration(gen))
		}
	}
	return s.switchTo(&Meta{Model: model, Version: version, Dim: len(centre), Centre: slices.Clone(centre), CompactedAt: time.Now(), Generation: gen})
}

// switchTo makes m, whose data files are written and synced, the store's:
// meta.json names them once it is replaced, and the files before are
// removed. If it cannot be replaced, the store stays as it was. The caller
// holds the lock.
func (s *Store) switchTo(m *Meta) error {
	if err := syncDir(s.dir); err != nil {
		return errors.Join(err, s.removeGeneration(m.Generation))
	}
	old := s.meta
	s.meta = m
	if err := s.writeMeta(); err != nil {
		s.meta = old
		return errors.Join(err, s.removeGeneration(m.Generation))
	}
	if err := s.closeFiles(); err != nil {
		return err
	}
	if old != nil {
		if err := s.removeGeneration(old.Generation); err != nil {
			return err
		}
	}
	return s.open()
}

// removeGeneration removes the data files of a generation.
func (s *Store) removeGeneration(gen int) error {
	var errs []error
	sf, vf := dataFiles(gen)
	for _, f := range []string{sf, vf} {
		if err := os.Remove(filepath.Join(s.dir, f)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// writeMeta replaces meta.json; the caller holds the lock.
func (s *Store) writeMeta() error {
	b, err := json.Marshal(s.meta)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(s.dir, metaFile), b); err != nil {
		return err
	}
	return syncDir(s.dir)
}

// syncDir syncs a directory, so that the renames and files made in it
// last.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}

func writeFile(path string, b []byte) error {
	tmp := path + ".new"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Append adds records to the end of the store and syncs them to disk.
func (s *Store) Append(recs []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.meta == nil {
		return errors.New("passage store: not begun")
	}
	dim := s.meta.Dim
	libs := len(s.meta.Libraries)
	sb := make([]byte, 0, len(recs)*signsSize(dim))
	vb := make([]byte, 0, len(recs)*vecSize(dim))
	for _, r := range recs {
		if len(r.Vector) != dim {
			return fmt.Errorf("passage store: a vector of %d dimensions, not %d", len(r.Vector), dim)
		}
		lib := s.libraryIndex(r.Library)
		if lib < 0 {
			return errors.New("passage store: too many libraries")
		}
		sb = binary.LittleEndian.AppendUint64(sb, uint64(r.Chunk))
		sb = binary.LittleEndian.AppendUint16(sb, uint16(lib))
		sb = append(sb, signs(r.Vector, s.meta.Centre)...)
		q, scale := quantize(r.Vector)
		vb = binary.LittleEndian.AppendUint32(vb, math.Float32bits(scale))
		for _, x := range q {
			vb = append(vb, byte(x))
		}
	}
	if len(s.meta.Libraries) != libs {
		if err := s.writeMeta(); err != nil {
			return err
		}
	}
	if _, err := s.signs.WriteAt(sb, int64(s.n*signsSize(dim))); err != nil {
		return err
	}
	if _, err := s.vecs.WriteAt(vb, int64(s.n*vecSize(dim))); err != nil {
		return err
	}
	if err := errors.Join(s.signs.Sync(), s.vecs.Sync()); err != nil {
		return err
	}
	s.n += len(recs)
	return nil
}

// libraryIndex is the library's place in meta, added if new; -1 when
// there is no room. The caller holds the lock.
func (s *Store) libraryIndex(id uuid.UUID) int {
	if i := slices.Index(s.meta.Libraries, id); i >= 0 {
		return i
	}
	if len(s.meta.Libraries) > math.MaxUint16 {
		return -1
	}
	s.meta.Libraries = append(s.meta.Libraries, id)
	return len(s.meta.Libraries) - 1
}

// signs is a bit a dimension, set where v is above the centre.
func signs(v, centre []float32) []byte {
	out := make([]byte, (len(v)+7)/8)
	for i, x := range v {
		if x > centre[i] {
			out[i/8] |= 1 << (i % 8)
		}
	}
	return out
}

// quantize is v as int8 with one scale: x ≈ q·scale.
func quantize(v []float32) ([]int8, float32) {
	var m float32
	for _, x := range v {
		m = max(m, float32(math.Abs(float64(x))))
	}
	if m == 0 {
		return make([]int8, len(v)), 0
	}
	scale := m / 127
	q := make([]int8, len(v))
	for i, x := range v {
		q[i] = int8(math.Round(float64(x / scale)))
	}
	return q, scale
}

// block is how many records a scan reads at a time.
const block = 1 << 14

// Search ranks the passages of the visible libraries by the signs, reads
// the int8 vectors of the best rescore of them, and returns the best keep
// of those by their cosine with q, each chunk once, the best first. A store
// of another model, or none, finds nothing.
func (s *Store) Search(q []float32, model, version string, visible []uuid.UUID, rescore, keep int) ([]Hit, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.meta
	if m == nil || m.Model != model || m.Version != version || len(q) != m.Dim || s.n == 0 {
		return nil, nil
	}
	sees := make([]bool, len(m.Libraries))
	seen := false
	for i, id := range m.Libraries {
		sees[i] = slices.Contains(visible, id)
		seen = seen || sees[i]
	}
	if !seen {
		return nil, nil
	}
	qs := signs(q, m.Centre)
	cand, err := s.scanSigns(qs, sees, rescore)
	if err != nil {
		return nil, err
	}
	// Read in the order they lie in the file.
	slices.SortFunc(cand, func(a, b scored) int { return a.i - b.i })
	vs := vecSize(m.Dim)
	buf := make([]byte, vs)
	best := map[int64]float64{}
	for _, c := range cand {
		if _, err := s.vecs.ReadAt(buf, int64(c.i*vs)); err != nil {
			return nil, err
		}
		scale := math.Float32frombits(binary.LittleEndian.Uint32(buf))
		var dot float32
		for d, x := range buf[4:] {
			dot += q[d] * float32(int8(x))
		}
		score := float64(dot * scale)
		if old, ok := best[c.chunk]; !ok || score > old {
			best[c.chunk] = score
		}
	}
	hits := make([]Hit, 0, len(best))
	for chunk, score := range best {
		hits = append(hits, Hit{chunk, score})
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		if a.Score != b.Score {
			if a.Score > b.Score {
				return -1
			}
			return 1
		}
		return int(a.Chunk - b.Chunk)
	})
	return hits[:min(keep, len(hits))], nil
}

// scored is a record by its place, with its chunk and the bits its signs
// share with the query's.
type scored struct {
	i     int
	chunk int64
	same  int
}

// scanSigns reads the signs file in blocks, several at once, and keeps the
// best k records of the visible libraries.
func (s *Store) scanSigns(qs []byte, sees []bool, k int) ([]scored, error) {
	size := signsSize(s.meta.Dim)
	blocks := (s.n + block - 1) / block
	workers := min(max(runtime.GOMAXPROCS(0)/2, 1), blocks)
	next := make(chan int)
	results := make([][]scored, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			buf := make([]byte, block*size)
			var top topK
			top.k = k
			for b := range next {
				lo := b * block
				n := min(block, s.n-lo)
				if _, err := s.signs.ReadAt(buf[:n*size], int64(lo*size)); err != nil && !errors.Is(err, io.EOF) {
					errs[w] = err
					continue
				}
				for r := range n {
					rec := buf[r*size : (r+1)*size]
					lib := binary.LittleEndian.Uint16(rec[8:])
					if int(lib) >= len(sees) || !sees[lib] {
						continue
					}
					top.add(scored{i: lo + r, chunk: int64(binary.LittleEndian.Uint64(rec)), same: same(qs, rec[10:])})
				}
			}
			results[w] = top.items
		})
	}
	for b := range blocks {
		next <- b
	}
	close(next)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	var top topK
	top.k = k
	for _, r := range results {
		for _, c := range r {
			top.add(c)
		}
	}
	return top.items, nil
}

// same counts the bits two sign strings share.
func same(a, b []byte) int {
	n := 0
	i := 0
	for ; i+8 <= len(a); i += 8 {
		n += 64 - bits.OnesCount64(binary.LittleEndian.Uint64(a[i:])^binary.LittleEndian.Uint64(b[i:]))
	}
	for ; i < len(a); i++ {
		n += 8 - bits.OnesCount8(a[i]^b[i])
	}
	return n
}

// topK keeps the k records with the most bits in common: a min-heap on
// same, the worst on top.
type topK struct {
	k     int
	items []scored
}

func (t *topK) add(c scored) {
	if len(t.items) < t.k {
		t.items = append(t.items, c)
		t.up(len(t.items) - 1)
		return
	}
	if t.k == 0 || c.same <= t.items[0].same {
		return
	}
	t.items[0] = c
	t.down(0)
}

func (t *topK) up(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if t.items[p].same <= t.items[i].same {
			return
		}
		t.items[p], t.items[i] = t.items[i], t.items[p]
		i = p
	}
}

func (t *topK) down(i int) {
	for {
		l, r, small := 2*i+1, 2*i+2, i
		if l < len(t.items) && t.items[l].same < t.items[small].same {
			small = l
		}
		if r < len(t.items) && t.items[r].same < t.items[small].same {
			small = r
		}
		if small == i {
			return
		}
		t.items[i], t.items[small] = t.items[small], t.items[i]
		i = small
	}
}

// Compact writes the store again with only the records live returns a
// library for, under that library: those of chunks gone or of files
// embedded since are dropped, and a book that moved library is found
// under its new one. Of a chunk stored twice (a file embedded again) the
// last record is kept.
func (s *Store) Compact(live func(chunk int64) (uuid.UUID, bool)) error {
	s.mu.Lock()
	m := s.meta
	n := s.n
	signs, vecs := s.signs, s.vecs
	s.last++
	gen := s.last
	s.mu.Unlock()
	if m == nil {
		return nil
	}
	dim := m.Dim
	ss := signsSize(dim)
	type place struct {
		chunk int64
		i     int
	}
	places := make([]place, 0, n)
	sbuf := make([]byte, block*ss)
	for lo := 0; lo < n; lo += block {
		c := min(block, n-lo)
		if _, err := signs.ReadAt(sbuf[:c*ss], int64(lo*ss)); err != nil {
			return err
		}
		for r := range c {
			places = append(places, place{int64(binary.LittleEndian.Uint64(sbuf[r*ss:])), lo + r})
		}
	}
	slices.SortFunc(places, func(a, b place) int {
		if a.chunk != b.chunk {
			return cmp.Compare(a.chunk, b.chunk)
		}
		return a.i - b.i
	})
	keep := make([]bool, n)
	for k, p := range places {
		if k+1 < len(places) && places[k+1].chunk == p.chunk {
			continue
		}
		_, keep[p.i] = live(p.chunk)
	}
	places = nil
	nm := &Meta{Model: m.Model, Version: m.Version, Dim: dim, Centre: m.Centre, CompactedAt: time.Now(), Generation: gen}
	if err := s.writeGeneration(nm, n, keep, live, signs, vecs); err != nil {
		return errors.Join(err, s.removeGeneration(gen))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.n != n || s.meta != m {
		return errors.Join(errors.New("passage store: changed while it was compacted"), s.removeGeneration(gen))
	}
	return s.switchTo(nm)
}

// writeGeneration writes the records of the n first to keep into the data
// files of nm's generation, under their libraries in nm, and syncs them.
func (s *Store) writeGeneration(nm *Meta, n int, keep []bool, live func(chunk int64) (uuid.UUID, bool), signs, vecs *os.File) error {
	ss, vs := signsSize(nm.Dim), vecSize(nm.Dim)
	sn, vn := dataFiles(nm.Generation)
	sf, err := os.OpenFile(filepath.Join(s.dir, sn), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer sf.Close()
	vf, err := os.OpenFile(filepath.Join(s.dir, vn), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer vf.Close()
	sbuf := make([]byte, block*ss)
	vbuf := make([]byte, block*vs)
	for lo := 0; lo < n; lo += block {
		c := min(block, n-lo)
		if _, err := signs.ReadAt(sbuf[:c*ss], int64(lo*ss)); err != nil {
			return err
		}
		if _, err := vecs.ReadAt(vbuf[:c*vs], int64(lo*vs)); err != nil {
			return err
		}
		var so, vo []byte
		for r := range c {
			rec := sbuf[r*ss : (r+1)*ss]
			if !keep[lo+r] {
				continue
			}
			lib, _ := live(int64(binary.LittleEndian.Uint64(rec)))
			idx := slices.Index(nm.Libraries, lib)
			if idx < 0 {
				nm.Libraries = append(nm.Libraries, lib)
				idx = len(nm.Libraries) - 1
			}
			so = append(so, rec[:8]...)
			so = binary.LittleEndian.AppendUint16(so, uint16(idx))
			so = append(so, rec[10:]...)
			vo = append(vo, vbuf[r*vs:(r+1)*vs]...)
		}
		if _, err := sf.Write(so); err != nil {
			return err
		}
		if _, err := vf.Write(vo); err != nil {
			return err
		}
	}
	return errors.Join(sf.Sync(), vf.Sync())
}

// Dir is where the store is kept under the data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, "passages") }
