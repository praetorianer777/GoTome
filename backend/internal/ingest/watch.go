package ingest

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// WatchTarget is a library whose folder is watched.
type WatchTarget struct {
	ID   uuid.UUID
	Root string
}

// WatchStatus is what the watcher holds, for the resources panel.
type WatchStatus struct {
	// Watches is the inotify watches taken: one per folder, not per file.
	Watches int
	// Limit is the kernel's fs.inotify.max_user_watches, 0 if not known.
	Limit int
	// Notified and Polled count the libraries watched either way.
	Notified, Polled int
	// LimitReached is set while a library is polled because the kernel
	// refused it more watches.
	LimitReached bool
}

// Watcher scans a library soon after files in its folder come or go (#409).
//
// A folder gets one inotify watch per directory (never per file, never
// below dot folders or the ones in ignoredFolders), all on one inotify
// instance. A library on a file system that does not report changes made
// elsewhere (network and FUSE mounts), or one the kernel has no more
// watches for, is polled instead: the modification times of its
// directories are compared every PollEvery. Either way a change only
// starts the clock; Request is called once the library has been quiet for
// Quiet, so a burst of copies is one scan, and the scan's own rules decide
// what is a book.
type Watcher struct {
	Targets func(ctx context.Context) ([]WatchTarget, error)
	Request func(ctx context.Context, libraryID uuid.UUID) error
	Log     *slog.Logger

	// Quiet is how long a library must have been still before it is scanned.
	Quiet time.Duration
	// MaxWait is the longest a change waits for the quiet, so an endless
	// copy cannot hold the scan off for ever.
	MaxWait time.Duration
	// PollEvery is how often the directories of a polled library are looked at.
	PollEvery time.Duration
	// SyncEvery is how often the set of libraries is read again.
	SyncEvery time.Duration
	Tick      time.Duration
	// Polling makes every library polled, as on a file system that cannot notify.
	Polling bool
	// addWatch replaces inotify_add_watch in tests.
	addWatch func(fd int, path string, mask uint32) (int, error)

	mu     sync.Mutex
	status WatchStatus

	in    *os.File
	byWD  map[int]*watchedDir
	libs  map[uuid.UUID]*watchedLibrary
	dirty map[uuid.UUID]*changed
}

type watchedLibrary struct {
	WatchTarget
	notify   bool
	wds      map[int]string
	snapshot map[string]time.Time
	polledAt time.Time
	limitHit bool
}

type watchedDir struct {
	lib  *watchedLibrary
	path string
}

type changed struct{ first, last time.Time }

const (
	watchMask = unix.IN_CREATE | unix.IN_DELETE | unix.IN_MOVED_FROM | unix.IN_MOVED_TO |
		unix.IN_CLOSE_WRITE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF | unix.IN_ONLYDIR

	maxUserWatchesFile = "/proc/sys/fs/inotify/max_user_watches"
)

// Status is what is watched now.
func (w *Watcher) Status() WatchStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status
}

type rawEvent struct {
	wd   int
	mask uint32
	name string
}

// Run watches until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	w.defaults()
	w.byWD = map[int]*watchedDir{}
	w.libs = map[uuid.UUID]*watchedLibrary{}
	w.dirty = map[uuid.UUID]*changed{}

	var events chan []rawEvent
	if !w.Polling {
		fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
		if err != nil {
			w.Log.Warn("folder watching: inotify is not available, the folders are polled", "error", err)
		} else {
			// A non-blocking descriptor goes through the runtime's poller,
			// so closing the file ends the reader's Read.
			w.in = os.NewFile(uintptr(fd), "inotify")
			events = make(chan []rawEvent, 64)
			go readEvents(w.in, events)
			defer w.in.Close()
		}
	}

	tick := time.NewTicker(w.Tick)
	defer tick.Stop()
	var synced time.Time
	for {
		if time.Since(synced) >= w.SyncEvery {
			w.sync(ctx)
			synced = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case batch := <-events:
			for _, e := range batch {
				w.handle(ctx, e)
			}
		case <-tick.C:
			w.poll(ctx)
			w.fire(ctx)
		}
		w.publish()
	}
}

func (w *Watcher) defaults() {
	set := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	set(&w.Quiet, 30*time.Second)
	set(&w.MaxWait, 10*time.Minute)
	set(&w.PollEvery, time.Minute)
	set(&w.SyncEvery, time.Minute)
	set(&w.Tick, time.Second)
	if w.Log == nil {
		w.Log = slog.Default()
	}
	if w.addWatch == nil {
		w.addWatch = unix.InotifyAddWatch
	}
}

func readEvents(in *os.File, out chan<- []rawEvent) {
	buf := make([]byte, 64*1024)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return
		}
		var batch []rawEvent
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			name := ""
			if ev.Len > 0 {
				raw := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
				name = strings.TrimRight(string(raw), "\x00")
			}
			batch = append(batch, rawEvent{wd: int(ev.Wd), mask: ev.Mask, name: name})
			off += unix.SizeofInotifyEvent + int(ev.Len)
		}
		out <- batch
	}
}

func (w *Watcher) sync(ctx context.Context) {
	targets, err := w.Targets(ctx)
	if err != nil {
		if ctx.Err() == nil {
			w.Log.Warn("folder watching: cannot list the libraries", "error", err)
		}
		return
	}
	want := make(map[uuid.UUID]WatchTarget, len(targets))
	for _, t := range targets {
		want[t.ID] = t
	}
	for id, lib := range w.libs {
		if t, ok := want[id]; !ok || t.Root != lib.Root {
			w.stop(lib)
			delete(w.libs, id)
			delete(w.dirty, id)
		}
	}
	for id, t := range want {
		if w.libs[id] == nil && ctx.Err() == nil {
			lib := &watchedLibrary{WatchTarget: t}
			w.libs[id] = lib
			w.start(ctx, lib)
		}
	}
}

func (w *Watcher) start(ctx context.Context, lib *watchedLibrary) {
	if w.in != nil && !onNetworkFilesystem(lib.Root) {
		lib.notify = true
		lib.wds = map[int]string{}
		w.addTree(ctx, lib, lib.Root)
		return
	}
	w.startPolling(ctx, lib)
}

func (w *Watcher) startPolling(ctx context.Context, lib *watchedLibrary) {
	lib.notify = false
	lib.snapshot, _ = directoryTimes(ctx, lib.Root)
	lib.polledAt = time.Now()
}

func (w *Watcher) stop(lib *watchedLibrary) {
	for wd := range lib.wds {
		if w.in != nil {
			_, _ = unix.InotifyRmWatch(int(w.in.Fd()), uint32(wd))
		}
		delete(w.byWD, wd)
	}
	lib.wds = nil
}

// addTree watches dir and the folders below it that a scan would enter. When
// the kernel has no watches left, the whole library is polled instead:
// half a tree watched would miss files without saying so.
func (w *Watcher) addTree(ctx context.Context, lib *watchedLibrary, dir string) {
	_ = filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if full != lib.Root && skippedFolder(d.Name()) {
			return filepath.SkipDir
		}
		wd, err := w.addWatch(int(w.in.Fd()), full, watchMask)
		switch {
		case err == nil:
			lib.wds[wd] = full
			w.byWD[wd] = &watchedDir{lib: lib, path: full}
		case errors.Is(err, unix.ENOSPC):
			w.Log.Warn("folder watching: the kernel's limit of inotify watches is reached, the library is polled instead; raise fs.inotify.max_user_watches on the host",
				"library", lib.ID, "watches", w.watchCount())
			w.stop(lib)
			lib.limitHit = true
			w.startPolling(ctx, lib)
			return filepath.SkipAll
		default:
			w.Log.Debug("folder watching: cannot watch", "path", full, "error", err)
			return filepath.SkipDir
		}
		return nil
	})
}

func (w *Watcher) watchCount() int { return len(w.byWD) }

func skippedFolder(name string) bool {
	return strings.HasPrefix(name, ".") || ignoredFolders[name]
}

func (w *Watcher) handle(ctx context.Context, e rawEvent) {
	if e.mask&unix.IN_Q_OVERFLOW != 0 {
		for _, lib := range w.libs {
			if lib.notify {
				w.mark(lib.ID)
			}
		}
		return
	}
	dir := w.byWD[e.wd]
	if dir == nil {
		return
	}
	lib := dir.lib
	if e.mask&unix.IN_IGNORED != 0 {
		delete(w.byWD, e.wd)
		delete(lib.wds, e.wd)
		return
	}
	if e.mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF) != 0 {
		w.mark(lib.ID)
		return
	}
	if strings.HasPrefix(e.name, ".") {
		return
	}
	if e.mask&unix.IN_ISDIR != 0 {
		if skippedFolder(e.name) {
			return
		}
		if e.mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
			w.addTree(ctx, lib, filepath.Join(dir.path, e.name))
		}
		w.mark(lib.ID)
		return
	}
	format := strings.ToLower(strings.TrimPrefix(filepath.Ext(e.name), "."))
	if _, known := catalog.KindOf(format); known {
		w.mark(lib.ID)
	}
}

func (w *Watcher) mark(id uuid.UUID) {
	now := time.Now()
	if c := w.dirty[id]; c != nil {
		c.last = now
		return
	}
	w.dirty[id] = &changed{first: now, last: now}
}

func (w *Watcher) poll(ctx context.Context) {
	for _, lib := range w.libs {
		if lib.notify || time.Since(lib.polledAt) < w.PollEvery {
			continue
		}
		lib.polledAt = time.Now()
		now, err := directoryTimes(ctx, lib.Root)
		if err != nil {
			continue
		}
		if !sameTimes(lib.snapshot, now) {
			w.mark(lib.ID)
		}
		lib.snapshot = now
	}
}

func (w *Watcher) fire(ctx context.Context) {
	for id, c := range w.dirty {
		now := time.Now()
		if now.Sub(c.last) < w.Quiet && now.Sub(c.first) < w.MaxWait {
			continue
		}
		if err := w.Request(ctx, id); err != nil {
			if ctx.Err() == nil {
				w.Log.Warn("folder watching: cannot ask for a scan", "library", id, "error", err)
			}
			continue
		}
		delete(w.dirty, id)
	}
}

func (w *Watcher) publish() {
	s := WatchStatus{Watches: len(w.byWD), Limit: maxUserWatches()}
	for _, lib := range w.libs {
		if lib.notify {
			s.Notified++
		} else {
			s.Polled++
		}
		s.LimitReached = s.LimitReached || lib.limitHit
	}
	w.mu.Lock()
	w.status = s
	w.mu.Unlock()
}

func maxUserWatches() int {
	b, err := os.ReadFile(maxUserWatchesFile)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// directoryTimes is the modification time of every folder a scan would
// enter: a file added, removed or renamed in a folder changes its own.
func directoryTimes(ctx context.Context, root string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			if full == root {
				return err
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if full != root && skippedFolder(d.Name()) {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out[full] = info.ModTime()
		return nil
	})
	return out, err
}

func sameTimes(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if o, ok := b[k]; !ok || !o.Equal(v) {
			return false
		}
	}
	return true
}

// Magic numbers of file systems that report changes made through the same
// kernel only, or none: a watch on them succeeds and then stays silent.
var silentFilesystems = map[int64]bool{
	0x6969:     true, // NFS
	0xFF534D42: true, // CIFS
	0xFE534D42: true, // SMB2
	0x65735546: true, // FUSE
	0x01021997: true, // 9p
	0x00C36400: true, // Ceph
}

func onNetworkFilesystem(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	return silentFilesystems[int64(st.Type)]
}
