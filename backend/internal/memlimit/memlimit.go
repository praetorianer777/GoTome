// Package memlimit holds the Go heap under the container's memory limit.
// The runtime does not read a cgroup's limit by itself: without a soft
// limit it lets the heap grow until the kernel kills the container, where
// collecting more often would have kept it within.
package memlimit

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// Share is the part of the container's limit the Go heap may take. The rest
// is for what the runtime does not count: ONNX Runtime's memory while books
// are embedded, and the programs extraction runs (poppler, ffmpeg), which
// share the container's limit.
const Share = 0.75

// files are where cgroup v2 and v1 say the limit, in that order.
var files = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// unlimited is above any limit a cgroup v1 file means as one: it says
// "no limit" as a number near the largest int64.
const unlimited = 1 << 60

// Apply sets the soft memory limit to Share of the container's, unless
// GOMEMLIMIT says otherwise or there is no limit. It returns the limit set,
// or 0.
func Apply() int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	limit := containerLimit(files)
	if limit <= 0 {
		return 0
	}
	soft := int64(float64(limit) * Share)
	debug.SetMemoryLimit(soft)
	return soft
}

// containerLimit is the first limit one of the files states, or 0.
func containerLimit(paths []string) int64 {
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(raw))
		if v == "max" {
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n >= unlimited {
			return 0
		}
		return n
	}
	return 0
}
