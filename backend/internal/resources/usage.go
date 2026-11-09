package resources

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
)

// Usage is the memory in use now, where it can be seen from here. A figure
// is 0 when it cannot be.
type Usage struct {
	// AppProcessBytes is the resident memory of the `gotome serve`
	// process.
	AppProcessBytes int64 `json:"appProcessBytes"`
	// AppContainerBytes is what the app's container holds in memory that
	// is not cache the kernel gives back, as `docker stats` counts.
	AppContainerBytes int64 `json:"appContainerBytes"`
	// PostgresContainerBytes is the same for the database's container,
	// read through the database, which a superuser may.
	PostgresContainerBytes int64 `json:"postgresContainerBytes"`
}

// cgroup files holding a container's memory: version 2, then version 1.
var (
	cgroupCurrent = []string{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory/memory.usage_in_bytes"}
	cgroupStat    = []string{"/sys/fs/cgroup/memory.stat", "/sys/fs/cgroup/memory/memory.stat"}
)

// Usage reads the memory now in use.
func (s *Service) Usage(ctx context.Context) Usage {
	u := Usage{AppProcessBytes: serveProcessBytes("/proc")}
	u.AppContainerBytes = workingSet(readFirst(os.ReadFile, cgroupCurrent), readFirst(os.ReadFile, cgroupStat))
	q := sqlc.New(s.pool)
	remote := func(path string) ([]byte, error) {
		content, err := q.ReadServerFile(ctx, path)
		return []byte(content), err
	}
	u.PostgresContainerBytes = workingSet(readFirst(remote, cgroupCurrent), readFirst(remote, cgroupStat))
	return u
}

// readFirst is the content of the first of paths that read.
func readFirst(read func(string) ([]byte, error), paths []string) string {
	for _, path := range paths {
		if raw, err := read(path); err == nil {
			return string(raw)
		}
	}
	return ""
}

// workingSet is the memory a cgroup holds less the file cache that is not
// in use, as `docker stats` counts it: memory.current less inactive_file.
func workingSet(current, stat string) int64 {
	used, err := strconv.ParseInt(strings.TrimSpace(current), 10, 64)
	if err != nil || used <= 0 {
		return 0
	}
	for _, line := range strings.Split(stat, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && (fields[0] == "inactive_file" || fields[0] == "total_inactive_file") {
			if idle, err := strconv.ParseInt(fields[1], 10, 64); err == nil && idle < used {
				return used - idle
			}
		}
	}
	return used
}

// serveProcessBytes is the resident memory of the process running
// `gotome serve` under the /proc at root, or 0 when there is none.
func serveProcessBytes(root string) int64 {
	dirs, _ := filepath.Glob(filepath.Join(root, "[0-9]*"))
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		args := bytes.Split(bytes.TrimRight(raw, "\x00"), []byte{0})
		if len(args) < 2 || filepath.Base(string(args[0])) != "gotome" || string(args[1]) != "serve" {
			continue
		}
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(status), "\n") {
			if rest, ok := strings.CutPrefix(line, "VmRSS:"); ok {
				fields := strings.Fields(rest)
				if len(fields) == 0 {
					continue
				}
				if kb, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
					return kb << 10
				}
			}
		}
	}
	return 0
}
