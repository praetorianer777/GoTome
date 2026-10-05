package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Memory watches the cgroup of the container the engine runs in. Anon is
// what the processes hold themselves; current also counts the page cache,
// which the kernel takes back at the limit, so anon is the figure that
// says whether an engine fits.
type Memory struct {
	dir string

	mu         sync.Mutex
	maxAnon    int64
	maxCurrent int64
	stop       chan struct{}
	done       chan struct{}
}

// MemoryPeak is what a phase took at its highest, in bytes.
type MemoryPeak struct {
	Anon    int64 `json:"anon"`
	Current int64 `json:"current"`
}

// WatchMemory samples the cgroup in dir until Stop. An empty dir watches
// nothing.
func WatchMemory(dir string) *Memory {
	m := &Memory{dir: dir, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(m.done)
		if dir == "" {
			return
		}
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			m.sample()
			select {
			case <-m.stop:
				return
			case <-tick.C:
			}
		}
	}()
	return m
}

func (m *Memory) sample() {
	anon := statValue(filepath.Join(m.dir, "memory.stat"), "anon")
	current := fileValue(filepath.Join(m.dir, "memory.current"))
	m.mu.Lock()
	m.maxAnon = max(m.maxAnon, anon)
	m.maxCurrent = max(m.maxCurrent, current)
	m.mu.Unlock()
}

// Stop ends the watch and returns the highest values seen.
func (m *Memory) Stop() MemoryPeak {
	close(m.stop)
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	return MemoryPeak{Anon: m.maxAnon, Current: m.maxCurrent}
}

// OOMKills is how often the kernel killed a process of the cgroup for
// want of memory.
func OOMKills(dir string) int64 {
	if dir == "" {
		return 0
	}
	return statValue(filepath.Join(dir, "memory.events"), "oom_kill")
}

func fileValue(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(string(bytes.TrimSpace(b)), 10, 64)
	return n
}

func statValue(path, key string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := bytes.Cut(sc.Bytes(), []byte(" "))
		if ok && string(k) == key {
			n, _ := strconv.ParseInt(string(v), 10, 64)
			return n
		}
	}
	return 0
}
