package passages

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// unit is a random vector of length 1 with a common part, as e5's have.
func unit(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var n float64
	for i := range v {
		v[i] = float32(rng.NormFloat64()) + 3
		n += float64(v[i]) * float64(v[i])
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(n))
	}
	return v
}

func cosine(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func begun(t *testing.T, dir string, vecs [][]float32) *Store {
	t.Helper()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Begin("m", "v", centre(vecs, len(vecs[0]))); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTheStoreFindsTheNearestPassagesOfTheVisibleLibraries(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	open, vault := uuid.New(), uuid.New()
	var vecs [][]float32
	var recs []Record
	for i := range 3000 {
		v := unit(rng, 64)
		vecs = append(vecs, v)
		lib := open
		if i%3 == 0 {
			lib = vault
		}
		recs = append(recs, Record{Chunk: int64(i + 1), Library: lib, Vector: v})
	}
	s := begun(t, t.TempDir(), vecs)
	defer s.Close()
	if err := s.Append(recs[:1000]); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(recs[1000:]); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 3000 || s.Size() != 3000*int64(signsSize(64)+vecSize(64)) {
		t.Fatalf("%d records, %d bytes", s.Len(), s.Size())
	}

	q := recs[4].Vector // of the open library
	hits, err := s.Search(q, "m", "v", []uuid.UUID{open}, 3000, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 10 || hits[0].Chunk != 5 || hits[0].Score < 0.99 {
		t.Fatalf("hits %v", hits)
	}
	// Rescoring every passage gives the exact order among the open ones.
	var want []int64
	order := slices.Clone(recs)
	slices.SortFunc(order, func(a, b Record) int {
		ca, cb := cosine(q, a.Vector), cosine(q, b.Vector)
		if ca > cb {
			return -1
		}
		if ca < cb {
			return 1
		}
		return 0
	})
	for _, r := range order {
		if r.Library == open && len(want) < 10 {
			want = append(want, r.Chunk)
		}
	}
	for i, h := range hits {
		if h.Chunk != want[i] {
			t.Errorf("hit %d: chunk %d, exact %d", i, h.Chunk, want[i])
		}
		if r := recs[h.Chunk-1]; r.Library != open || math.Abs(h.Score-cosine(q, r.Vector)) > 0.02 {
			t.Errorf("hit %v of %v, cosine %.3f", h, r.Library, cosine(q, r.Vector))
		}
	}
	// Rescoring a tenth of the visible ones still finds half of them, where
	// a tenth taken at random would hold one.
	few, _ := s.Search(q, "m", "v", []uuid.UUID{open}, 200, 10)
	found := 0
	for _, h := range few {
		if slices.Contains(want, h.Chunk) {
			found++
		}
	}
	if found < 5 {
		t.Errorf("rescoring 200 found %d of the ten best", found)
	}
	if hits, _ := s.Search(q, "m", "v", []uuid.UUID{uuid.New()}, 3000, 10); len(hits) != 0 {
		t.Errorf("a library the store does not hold: %v", hits)
	}
	if hits, _ := s.Search(q, "other", "v", []uuid.UUID{open}, 3000, 10); len(hits) != 0 {
		t.Errorf("another model: %v", hits)
	}
}

func TestTheStoreSurvivesAHalfWrittenRecordAndCompacts(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	dir := t.TempDir()
	a, b := uuid.New(), uuid.New()
	var vecs [][]float32
	for range 10 {
		vecs = append(vecs, unit(rng, 16))
	}
	s := begun(t, dir, vecs)
	var recs []Record
	for i, v := range vecs {
		recs = append(recs, Record{Chunk: int64(i), Library: a, Vector: v})
	}
	if err := s.Append(recs); err != nil {
		t.Fatal(err)
	}
	// Chunk 3 embedded again, as when a file is.
	if err := s.Append([]Record{{Chunk: 3, Library: a, Vector: vecs[9]}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	sf, _ := dataFiles(s.Meta().Generation)
	f, err := os.OpenFile(filepath.Join(dir, sf), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{1, 2, 3})
	f.Close()

	s, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Len() != 11 {
		t.Fatalf("reopened with %d records", s.Len())
	}
	before := s.Meta().CompactedAt
	time.Sleep(time.Millisecond)
	// Chunks 0 to 5 live, and moved to library b.
	err = s.Compact(func(chunk int64) (uuid.UUID, bool) { return b, chunk <= 5 })
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 6 || !s.Meta().CompactedAt.After(before) {
		t.Fatalf("%d records after compacting, %v", s.Len(), s.Meta())
	}
	if hits, _ := s.Search(vecs[2], "m", "v", []uuid.UUID{a}, 10, 10); len(hits) != 0 {
		t.Errorf("found under the old library: %v", hits)
	}
	hits, err := s.Search(vecs[9], "m", "v", []uuid.UUID{b}, 10, 10)
	if err != nil || len(hits) != 6 || hits[0].Chunk != 3 {
		t.Errorf("chunk 3 is its second vector: %v %v", hits, err)
	}
	if err := s.Begin("n", "w", vecs[0]); err != nil || s.Len() != 0 || s.Meta().Model != "n" {
		t.Errorf("begun again: %d %v %v", s.Len(), s.Meta(), err)
	}
}

// stored is a store in dir begun with ten records of library a, closed.
func stored(t *testing.T, dir string, a uuid.UUID) [][]float32 {
	t.Helper()
	rng := rand.New(rand.NewPCG(7, 8))
	var vecs [][]float32
	var recs []Record
	for i := range 10 {
		vecs = append(vecs, unit(rng, 16))
		recs = append(recs, Record{Chunk: int64(i), Library: a, Vector: vecs[i]})
	}
	s := begun(t, dir, vecs)
	if err := s.Append(recs); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return vecs
}

func reopen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func writeMetaFile(t *testing.T, dir string, m *Meta) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, metaFile), b, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestACompactionCutOffBeforeItsSwitchLeavesTheStoreAsItWas(t *testing.T) {
	dir := t.TempDir()
	a := uuid.New()
	vecs := stored(t, dir, a)
	// The next generation's files half written: meta.json still names the
	// one before.
	sf, vf := dataFiles(2)
	os.WriteFile(filepath.Join(dir, sf), make([]byte, signsSize(16)*3), 0o640)
	os.WriteFile(filepath.Join(dir, vf), make([]byte, 7), 0o640)

	s := reopen(t, dir)
	if s.StartedOver() || s.Len() != 10 || s.Meta().Generation != 1 {
		t.Fatalf("started over %v, %d records, %+v", s.StartedOver(), s.Len(), s.Meta())
	}
	if got := names(t, dir); !slices.Equal(got, []string{metaFile, signsFile + ".1", vecsFile + ".1"}) {
		t.Errorf("files left: %v", got)
	}
	hits, err := s.Search(vecs[4], "m", "v", []uuid.UUID{a}, 10, 1)
	if err != nil || len(hits) != 1 || hits[0].Chunk != 4 {
		t.Errorf("hits %v %v", hits, err)
	}
	if err := s.Compact(func(chunk int64) (uuid.UUID, bool) { return a, chunk < 5 }); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 5 || s.Meta().Generation != 2 {
		t.Fatalf("%d records, %+v", s.Len(), s.Meta())
	}
	if got := names(t, dir); !slices.Equal(got, []string{metaFile, signsFile + ".2", vecsFile + ".2"}) {
		t.Errorf("files after compacting: %v", got)
	}
}

func TestAStoreWhoseFilesAreNotOneSetStartsOver(t *testing.T) {
	a := uuid.New()
	for _, tc := range []struct {
		name  string
		crash func(t *testing.T, dir string)
		over  bool
	}{
		{"meta.json names files that are not there", func(t *testing.T, dir string) {
			s := reopen(t, dir)
			m := s.Meta()
			s.Close()
			m.Generation = 5
			writeMetaFile(t, dir, m)
		}, true},
		{"a store before generations", func(t *testing.T, dir string) {
			legacy(t, dir)
		}, false},
		{"one before generations, cut off while writing", func(t *testing.T, dir string) {
			legacy(t, dir)
			os.WriteFile(filepath.Join(dir, signsFile+".new"), nil, 0o640)
			os.WriteFile(filepath.Join(dir, vecsFile+".new"), nil, 0o640)
		}, false},
		{"one before generations, cut off between its renames", func(t *testing.T, dir string) {
			legacy(t, dir)
			// The signs renamed into place, the vectors not yet.
			os.WriteFile(filepath.Join(dir, vecsFile+".new"), nil, 0o640)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			vecs := stored(t, dir, a)
			tc.crash(t, dir)
			s := reopen(t, dir)
			if s.StartedOver() != tc.over {
				t.Fatalf("started over: %v", s.StartedOver())
			}
			if tc.over {
				if s.Meta() != nil || s.Len() != 0 || len(names(t, dir)) != 0 {
					t.Fatalf("%+v, %d records, files %v", s.Meta(), s.Len(), names(t, dir))
				}
				if hits, err := s.Search(vecs[0], "m", "v", []uuid.UUID{a}, 10, 1); err != nil || len(hits) != 0 {
					t.Errorf("hits %v %v", hits, err)
				}
				if err := s.Begin("m", "v", vecs[0]); err != nil {
					t.Fatal(err)
				}
				if err := s.Append([]Record{{Chunk: 1, Library: a, Vector: vecs[1]}}); err != nil || s.Len() != 1 {
					t.Fatalf("appended after starting over: %d %v", s.Len(), err)
				}
				return
			}
			if s.Len() != 10 || s.Meta().Generation != 0 {
				t.Fatalf("%d records, %+v", s.Len(), s.Meta())
			}
			if got := names(t, dir); !slices.Equal(got, []string{metaFile, signsFile, vecsFile}) {
				t.Errorf("files left: %v", got)
			}
			if err := s.Compact(func(int64) (uuid.UUID, bool) { return a, true }); err != nil {
				t.Fatal(err)
			}
			if s.Len() != 10 || s.Meta().Generation != 1 {
				t.Fatalf("compacted to %d records, %+v", s.Len(), s.Meta())
			}
			if got := names(t, dir); !slices.Equal(got, []string{metaFile, signsFile + ".1", vecsFile + ".1"}) {
				t.Errorf("files after compacting: %v", got)
			}
		})
	}
}

// legacy turns the store in dir into one of before generations.
func legacy(t *testing.T, dir string) {
	t.Helper()
	s := reopen(t, dir)
	m := s.Meta()
	s.Close()
	sf, vf := dataFiles(m.Generation)
	if err := errors.Join(os.Rename(filepath.Join(dir, sf), filepath.Join(dir, signsFile)), os.Rename(filepath.Join(dir, vf), filepath.Join(dir, vecsFile))); err != nil {
		t.Fatal(err)
	}
	m.Generation = 0
	writeMetaFile(t, dir, m)
}

func TestMetaIsACopyWhileRecordsAreAppended(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	var vecs [][]float32
	for range 4 {
		vecs = append(vecs, unit(rng, 16))
	}
	s := begun(t, t.TempDir(), vecs)
	defer s.Close()
	first := uuid.New()
	if err := s.Append([]Record{{Chunk: 1, Library: first, Vector: vecs[0]}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 50 {
			if err := s.Append([]Record{{Chunk: int64(i + 2), Library: uuid.New(), Vector: vecs[i%4]}}); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Go(func() {
		for range 50 {
			m := s.Meta()
			m.Libraries[0] = uuid.Nil
			m.Libraries = append(m.Libraries, uuid.Nil)
		}
	})
	wg.Wait()
	m := s.Meta()
	if len(m.Libraries) != 51 || m.Libraries[0] != first || slices.Contains(m.Libraries, uuid.Nil) {
		t.Errorf("the store's libraries were changed through a copy: %d, first %v", len(m.Libraries), m.Libraries[0])
	}
}

// BenchmarkSearch times a search over GOTOME_BENCH_PASSAGES passages
// (100,000 by default) of 384 dimensions, half of them visible, rescoring
// a fiftieth of them as the service does.
func BenchmarkSearch(b *testing.B) {
	n := 100000
	if v, err := strconv.Atoi(os.Getenv("GOTOME_BENCH_PASSAGES")); err == nil {
		n = v
	}
	rng := rand.New(rand.NewPCG(5, 6))
	libs := []uuid.UUID{uuid.New(), uuid.New()}
	dir := b.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	var sample [][]float32
	for range 100 {
		sample = append(sample, unit(rng, 384))
	}
	if err := s.Begin("m", "v", centre(sample, 384)); err != nil {
		b.Fatal(err)
	}
	for lo := 0; lo < n; lo += 10000 {
		recs := make([]Record, 0, 10000)
		for i := lo; i < min(lo+10000, n); i++ {
			recs = append(recs, Record{Chunk: int64(i), Library: libs[i%2], Vector: unit(rng, 384)})
		}
		if err := s.Append(recs); err != nil {
			b.Fatal(err)
		}
	}
	q := unit(rng, 384)
	b.ResetTimer()
	for b.Loop() {
		if _, err := s.Search(q, "m", "v", libs[:1], max(rescoreMin, n/50), keep); err != nil {
			b.Fatal(err)
		}
	}
}
