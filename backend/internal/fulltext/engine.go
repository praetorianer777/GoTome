//go:build cgo

package fulltext

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct engine engine;

static void *lib;
static const char *(*p_version)(void);
static int (*p_open)(const char *, uint64_t, engine **, char **);
static void (*p_close)(engine *);
static int (*p_apply)(engine *, const char *, char **);
static int (*p_commit)(engine *, char **);
static int (*p_reload)(engine *, char **);
static int (*p_search)(engine *, const char *, char **, char **);
static int (*p_info)(engine *, char **, char **);
static int (*p_snippets)(engine *, const char *, char **, char **);
static void (*p_free)(char *);

static const char *gtf_load(const char *path) {
	lib = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (!lib) return dlerror();
#define SYM(p, name) if (!(*(void **)(&p) = dlsym(lib, name))) return "the library has no " name
	SYM(p_version, "gtf_version");
	SYM(p_open, "gtf_open");
	SYM(p_close, "gtf_close");
	SYM(p_apply, "gtf_apply");
	SYM(p_commit, "gtf_commit");
	SYM(p_reload, "gtf_reload");
	SYM(p_search, "gtf_search");
	SYM(p_info, "gtf_info");
	SYM(p_snippets, "gtf_snippets");
	SYM(p_free, "gtf_free");
	return NULL;
}

static const char *call_version(void) { return p_version(); }
static int call_open(const char *dir, uint64_t heap, engine **out, char **err) { return p_open(dir, heap, out, err); }
static void call_close(engine *e) { p_close(e); }
static int call_apply(engine *e, const char *batch, char **err) { return p_apply(e, batch, err); }
static int call_commit(engine *e, char **err) { return p_commit(e, err); }
static int call_reload(engine *e, char **err) { return p_reload(e, err); }
static int call_search(engine *e, const char *q, char **out, char **err) { return p_search(e, q, out, err); }
static int call_info(engine *e, char **out, char **err) { return p_info(e, out, err); }
static int call_snippets(engine *e, const char *r, char **out, char **err) { return p_snippets(e, r, out, err); }
static void call_free(char *s) { p_free(s); }
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/google/uuid"
)

var (
	loadOnce sync.Once
	loadPath string
	loadErr  error
)

// Load loads the engine's library from path, once per process, and returns
// its version. A process loads one library: a later call with another path
// gets the first.
func Load(path string) (string, error) {
	loadOnce.Do(func() {
		loadPath = path
		cpath := C.CString(path)
		defer C.free(unsafe.Pointer(cpath))
		if msg := C.gtf_load(cpath); msg != nil {
			loadErr = fmt.Errorf("%w: load %s: %s", ErrUnavailable, path, C.GoString(msg))
		}
	})
	if loadErr != nil {
		return "", loadErr
	}
	return C.GoString(C.call_version()), nil
}

// Index is an open index. It is safe for concurrent use; Close waits for
// the calls in progress.
type Index struct {
	mu sync.RWMutex
	e  *C.engine
}

// Open opens the index in dir, making it when the folder holds none, with
// the library Load loaded. heap is the writer's buffer in bytes; 0 opens the
// index for searching only, which any number of processes may.
func Open(dir string, heap int64) (*Index, error) {
	if loadPath == "" {
		return nil, fmt.Errorf("%w: the library is not loaded", ErrUnavailable)
	}
	if loadErr != nil {
		return nil, loadErr
	}
	cdir := C.CString(dir)
	defer C.free(unsafe.Pointer(cdir))
	var e *C.engine
	var cerr *C.char
	if C.call_open(cdir, C.uint64_t(max(heap, 0)), &e, &cerr) != 0 {
		return nil, fmt.Errorf("open the full-text index in %s: %w", dir, failure(cerr))
	}
	return &Index{e: e}, nil
}

func failure(cerr *C.char) error {
	if cerr == nil {
		return errors.New("the engine failed without saying why")
	}
	defer C.call_free(cerr)
	return errors.New(C.GoString(cerr))
}

var errClosed = errors.New("the full-text index is closed")

// Apply hands the batch to the writer; it is found after the next Commit.
func (ix *Index) Apply(b Batch) error {
	if len(b.DeleteChunks)+len(b.DeleteBooks)+len(b.DeleteLibraries)+len(b.Add) == 0 {
		return nil
	}
	data, err := json.Marshal(b)
	if err != nil {
		return err
	}
	cdata := C.CString(string(data))
	defer C.free(unsafe.Pointer(cdata))
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.e == nil {
		return errClosed
	}
	var cerr *C.char
	if C.call_apply(ix.e, cdata, &cerr) != 0 {
		return fmt.Errorf("apply to the full-text index: %w", failure(cerr))
	}
	return nil
}

// Commit makes what was applied durable and found.
func (ix *Index) Commit() error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.e == nil {
		return errClosed
	}
	var cerr *C.char
	if C.call_commit(ix.e, &cerr) != 0 {
		return fmt.Errorf("commit the full-text index: %w", failure(cerr))
	}
	return nil
}

// Reload looks for what another process committed since; the writer's own
// commits are found at once.
func (ix *Index) Reload() error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.e == nil {
		return errClosed
	}
	var cerr *C.char
	if C.call_reload(ix.e, &cerr) != 0 {
		return fmt.Errorf("reload the full-text index: %w", failure(cerr))
	}
	return nil
}

// Search returns the best chunks that match, best first.
func (ix *Index) Search(q Query) ([]Hit, error) {
	if q.Libraries == nil {
		return nil, nil
	}
	data, err := json.Marshal(q)
	if err != nil {
		return nil, err
	}
	out, err := ix.call(func(cout **C.char, cerr **C.char) C.int {
		cq := C.CString(string(data))
		defer C.free(unsafe.Pointer(cq))
		return C.call_search(ix.e, cq, cout, cerr)
	})
	if err != nil {
		return nil, fmt.Errorf("search the full-text index: %w", err)
	}
	var hits []Hit
	return hits, json.Unmarshal(out, &hits)
}

// Snippets makes a snippet of each passage around what the query matches,
// of about maxChars characters.
func (ix *Index) Snippets(q Query, passages []Passage, maxChars int) ([]Snippet, error) {
	if q.Libraries == nil {
		q.Libraries = []uuid.UUID{}
	}
	data, err := json.Marshal(snippetRequest{Query: q, Passages: passages, MaxChars: maxChars})
	if err != nil {
		return nil, err
	}
	out, err := ix.call(func(cout **C.char, cerr **C.char) C.int {
		cr := C.CString(string(data))
		defer C.free(unsafe.Pointer(cr))
		return C.call_snippets(ix.e, cr, cout, cerr)
	})
	if err != nil {
		return nil, fmt.Errorf("make snippets: %w", err)
	}
	var got []Snippet
	return got, json.Unmarshal(out, &got)
}

// Info says what the index holds.
func (ix *Index) Info() (Info, error) {
	out, err := ix.call(func(cout **C.char, cerr **C.char) C.int {
		return C.call_info(ix.e, cout, cerr)
	})
	if err != nil {
		return Info{}, fmt.Errorf("read the full-text index: %w", err)
	}
	var info Info
	return info, json.Unmarshal(out, &info)
}

func (ix *Index) call(f func(cout **C.char, cerr **C.char) C.int) ([]byte, error) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if ix.e == nil {
		return nil, errClosed
	}
	var cout, cerr *C.char
	if f(&cout, &cerr) != 0 {
		return nil, failure(cerr)
	}
	defer C.call_free(cout)
	return []byte(C.GoString(cout)), nil
}

// Close closes the index and gives its writer up; what was applied and not
// committed is dropped.
func (ix *Index) Close() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.e != nil {
		C.call_close(ix.e)
		ix.e = nil
	}
	return nil
}
