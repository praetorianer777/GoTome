// Package resources checks whether the machine GOtome runs on has room for
// what it is set up to use (#240): the memory Postgres is told it may take,
// beside the app and the embedding model, and the free space of the data
// directory. Postgres's settings live in its data volume and travel with
// it, sized for the machine the database was first made on.
//
// Postgres runs in a container of its own, whose limit the app cannot see.
// The check compares against the memory the app's container sees, which on
// a machine that runs both is the machine's, and against the disk of
// GOTOME_DATA_DIR, which is usually the one Docker keeps Postgres's volume
// on too.
package resources

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/memlimit"
)

// The rules' thresholds.
const (
	// AppBytes is what the app takes besides an embedding model: the Go
	// heap, the full-text index's writer and the programs extraction runs.
	AppBytes = 512 << 20
	// Budget is the share of the machine's memory Postgres, the app and the
	// model may take together. The rest is for the kernel, the file cache
	// Postgres and the full-text index read through, and Postgres's
	// processes besides their buffers.
	Budget = 0.8
	// SharedShare is the share of the machine's memory suggested for
	// shared_buffers, EffectiveCacheShare that for effective_cache_size.
	SharedShare         = 0.2
	EffectiveCacheShare = 0.5
	// MaintenanceCap and WorkMemCap are the most suggested for
	// maintenance_work_mem and work_mem: more helps a library of GOtome's
	// size little.
	MaintenanceCap = 256 << 20
	WorkMemCap     = 16 << 20
	// MinFreeBytes and MinFreeShare: less free space than either on the
	// data directory's disk is a warning. Postgres stops when its disk is
	// full, and recovers only once there is room.
	MinFreeBytes = 10 << 30
	MinFreeShare = 0.1
)

// The least suggested for each setting, Postgres's own defaults or near.
const (
	minShared      = 128 << 20
	minMaintenance = 64 << 20
	minWorkMem     = 4 << 20
)

// What a warning is about.
const (
	// KindMemory: Postgres's memory, with the app's and the model's, is
	// over the budget.
	KindMemory = "memory"
	// KindEmbedding: the embedding model alone leaves Postgres no room.
	KindEmbedding = "embedding"
	// KindParallel: Postgres may start more parallel workers than there
	// are CPUs.
	KindParallel = "parallel"
	// KindDisk: the data directory's disk is nearly full.
	KindDisk = "disk"
)

// Memory is the memory the app's container sees and what the parts take.
type Memory struct {
	// MachineBytes is the container's limit or, without one, the machine's
	// memory (MemTotal); 0 when neither can be read.
	MachineBytes int64 `json:"machineBytes"`
	// Limited says MachineBytes is the container's limit.
	Limited bool `json:"limited"`
	CPUs    int  `json:"cpus"`
	// AppBytes is what the app takes besides the model, an estimate.
	AppBytes int64 `json:"appBytes"`
	// EmbeddingModel is the model books are embedded with on this
	// machine's CPU, "" when embedding is off or runs on a server.
	EmbeddingModel string `json:"embeddingModel"`
	EmbeddingBytes int64  `json:"embeddingBytes"`
	// NeedBytes is Postgres's, the app's and the model's together;
	// BudgetBytes the Budget share of MachineBytes.
	NeedBytes   int64 `json:"needBytes"`
	BudgetBytes int64 `json:"budgetBytes"`
}

// Postgres is what Postgres is set to take.
type Postgres struct {
	SharedBuffersBytes      int64 `json:"sharedBuffersBytes"`
	WorkMemBytes            int64 `json:"workMemBytes"`
	MaintenanceWorkMemBytes int64 `json:"maintenanceWorkMemBytes"`
	EffectiveCacheSizeBytes int64 `json:"effectiveCacheSizeBytes"`
	MaxConnections          int   `json:"maxConnections"`
	// Connections is how many of them the app holds at most, each of which
	// may sort with work_mem at once.
	Connections                   int `json:"connections"`
	MaxParallelWorkers            int `json:"maxParallelWorkers"`
	MaxParallelWorkersPerGather   int `json:"maxParallelWorkersPerGather"`
	MaxParallelMaintenanceWorkers int `json:"maxParallelMaintenanceWorkers"`
	// NeedBytes is shared_buffers, one maintenance_work_mem (a VACUUM) and
	// work_mem for each of Connections.
	NeedBytes int64 `json:"needBytes"`
}

// Disk is the space of the data directory's disk.
type Disk struct {
	Path       string `json:"path"`
	FreeBytes  int64  `json:"freeBytes"`
	TotalBytes int64  `json:"totalBytes"`
	// DatabaseBytes is the database's size as Postgres tells it; its volume
	// is not visible to the app. IndexBytes is the full-text index's.
	DatabaseBytes int64 `json:"databaseBytes"`
	IndexBytes    int64 `json:"indexBytes"`
	// NeedFreeBytes is the least free space that is no warning: MinFreeBytes,
	// MinFreeShare of the disk, or room for the database and the index
	// again (a VACUUM FULL, a rebuild of the index), whichever is most.
	NeedFreeBytes int64 `json:"needFreeBytes"`
}

// Warning is one thing that does not fit, with the commands that make it
// fit where there are any: SQL statements to run in the database.
type Warning struct {
	Kind     string   `json:"kind"`
	Commands []string `json:"commands"`
}

// Health is what the check found.
type Health struct {
	Memory   Memory    `json:"memory"`
	Postgres Postgres  `json:"postgres"`
	Disk     Disk      `json:"disk"`
	Warnings []Warning `json:"warnings"`
}

// Evaluate applies the rules to the figures, which it completes with the
// sums and the budget.
func Evaluate(h Health) Health {
	pg := &h.Postgres
	pg.Connections = min(pg.Connections, pg.MaxConnections)
	pg.NeedBytes = pg.SharedBuffersBytes + pg.MaintenanceWorkMemBytes + pg.WorkMemBytes*int64(pg.Connections)
	m := &h.Memory
	m.NeedBytes = pg.NeedBytes + m.AppBytes + m.EmbeddingBytes
	m.BudgetBytes = int64(float64(m.MachineBytes) * Budget)
	d := &h.Disk
	d.NeedFreeBytes = max(MinFreeBytes, int64(float64(d.TotalBytes)*MinFreeShare), d.DatabaseBytes+d.IndexBytes)
	h.Warnings = []Warning{}

	if m.MachineBytes > 0 {
		if m.EmbeddingBytes > 0 && m.AppBytes+m.EmbeddingBytes+minShared+minMaintenance+minWorkMem*int64(pg.Connections) > m.BudgetBytes {
			h.Warnings = append(h.Warnings, Warning{Kind: KindEmbedding, Commands: []string{}})
		}
		if m.NeedBytes > m.BudgetBytes {
			h.Warnings = append(h.Warnings, Warning{Kind: KindMemory, Commands: memoryCommands(h)})
		}
	}
	if m.CPUs > 0 && max(pg.MaxParallelWorkers, pg.MaxParallelWorkersPerGather, pg.MaxParallelMaintenanceWorkers) > m.CPUs {
		h.Warnings = append(h.Warnings, Warning{Kind: KindParallel, Commands: parallelCommands(h)})
	}
	if d.TotalBytes > 0 && d.FreeBytes < d.NeedFreeBytes {
		h.Warnings = append(h.Warnings, Warning{Kind: KindDisk, Commands: []string{}})
	}
	return h
}

// memoryCommands lowers what is above the values that fit: the room the
// app and the model leave in the budget, half for shared_buffers, a
// quarter each for maintenance_work_mem and work_mem.
func memoryCommands(h Health) []string {
	m, pg := h.Memory, h.Postgres
	room := max(m.BudgetBytes-m.AppBytes-m.EmbeddingBytes, 0)
	shared := max(min(int64(float64(m.MachineBytes)*SharedShare), room/2), minShared)
	maintenance := max(min(MaintenanceCap, room/4), minMaintenance)
	work := int64(WorkMemCap)
	if pg.Connections > 0 {
		work = max(min(work, room/4/int64(pg.Connections)), minWorkMem)
	}
	cache := int64(float64(m.MachineBytes) * EffectiveCacheShare)
	out := []string{}
	lower := func(name string, have, want int64) {
		if want = want >> 20 << 20; have > want {
			out = append(out, fmt.Sprintf("ALTER SYSTEM SET %s = '%dMB';", name, want>>20))
		}
	}
	lower("shared_buffers", pg.SharedBuffersBytes, shared)
	lower("maintenance_work_mem", pg.MaintenanceWorkMemBytes, maintenance)
	lower("work_mem", pg.WorkMemBytes, work)
	lower("effective_cache_size", pg.EffectiveCacheSizeBytes, cache)
	return out
}

// parallelCommands keeps Postgres's parallel workers to the CPUs, a query's
// and a maintenance command's to half of them.
func parallelCommands(h Health) []string {
	pg, cpus := h.Postgres, h.Memory.CPUs
	half := max(cpus/2, 1)
	out := []string{}
	lower := func(name string, have, want int) {
		if have > want {
			out = append(out, fmt.Sprintf("ALTER SYSTEM SET %s = %d;", name, want))
		}
	}
	lower("max_parallel_workers", pg.MaxParallelWorkers, cpus)
	lower("max_parallel_workers_per_gather", pg.MaxParallelWorkersPerGather, half)
	lower("max_parallel_maintenance_workers", pg.MaxParallelMaintenanceWorkers, half)
	return out
}

// Service reads the figures and checks them.
type Service struct {
	pool *pgxpool.Pool
	// DataDir is GOTOME_DATA_DIR, IndexDir the full-text index's folder.
	DataDir  string
	IndexDir string
	// Embedding says which model books are embedded with and whether they
	// are; Server the embedding server's address, "" for none.
	Embedding func(ctx context.Context) (embed.Spec, bool, error)
	Server    func(ctx context.Context) (string, error)
	// memory reads the machine's memory and the container's limit; tests
	// replace it.
	memory func() (machine, limit int64)
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, memory: func() (int64, int64) { return memTotal("/proc/meminfo"), memlimit.Limit() }}
}

// The Postgres settings read.
var settingNames = []string{
	"shared_buffers", "work_mem", "maintenance_work_mem", "effective_cache_size", "max_connections",
	"max_parallel_workers", "max_parallel_workers_per_gather", "max_parallel_maintenance_workers",
}

// Check reads the figures and applies the rules.
func (s *Service) Check(ctx context.Context) (Health, error) {
	var h Health
	machine, limit := s.memory()
	h.Memory = Memory{MachineBytes: machine, CPUs: runtime.NumCPU(), AppBytes: AppBytes}
	if limit > 0 && (machine == 0 || limit < machine) {
		h.Memory.MachineBytes, h.Memory.Limited = limit, true
	}
	if s.Embedding != nil {
		spec, on, err := s.Embedding(ctx)
		if err != nil {
			return Health{}, err
		}
		server := ""
		if s.Server != nil && spec.Server != "" {
			if server, err = s.Server(ctx); err != nil {
				return Health{}, err
			}
		}
		if on && spec.Runs() && server == "" {
			h.Memory.EmbeddingModel, h.Memory.EmbeddingBytes = spec.Model.Name, spec.Memory
		}
	}

	q := sqlc.New(s.pool)
	rows, err := q.PostgresSettings(ctx, settingNames)
	if err != nil {
		return Health{}, err
	}
	pg := &h.Postgres
	for _, r := range rows {
		switch r.Name {
		case "shared_buffers":
			pg.SharedBuffersBytes = r.Value
		case "work_mem":
			pg.WorkMemBytes = r.Value
		case "maintenance_work_mem":
			pg.MaintenanceWorkMemBytes = r.Value
		case "effective_cache_size":
			pg.EffectiveCacheSizeBytes = r.Value
		case "max_connections":
			pg.MaxConnections = int(r.Value)
		case "max_parallel_workers":
			pg.MaxParallelWorkers = int(r.Value)
		case "max_parallel_workers_per_gather":
			pg.MaxParallelWorkersPerGather = int(r.Value)
		case "max_parallel_maintenance_workers":
			pg.MaxParallelMaintenanceWorkers = int(r.Value)
		}
	}
	pg.Connections = int(s.pool.Config().MaxConns)

	if h.Disk.DatabaseBytes, err = q.DatabaseSize(ctx); err != nil {
		return Health{}, err
	}
	h.Disk.Path = s.DataDir
	if s.DataDir != "" {
		h.Disk.FreeBytes, h.Disk.TotalBytes = diskSpace(s.DataDir)
	}
	if s.IndexDir != "" {
		h.Disk.IndexBytes = folderSize(s.IndexDir)
	}
	return Evaluate(h), nil
}

// memTotal is the machine's memory as /proc/meminfo says it, or 0. In an
// LXC container with lxcfs it is the container's.
func memTotal(path string) int64 {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb << 10
		}
	}
	return 0
}

// diskSpace is the free space an unprivileged process may use and the
// size of the disk dir is on; 0 for both when it cannot be read.
func diskSpace(dir string) (free, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize)
}

// folderSize adds up the files under dir; what cannot be read counts 0.
func folderSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			n += info.Size()
		}
		return nil
	})
	return n
}

// key tells one state of the warnings from another for the log.
func (h Health) key() string {
	parts := make([]string, 0, len(h.Warnings))
	for _, w := range h.Warnings {
		parts = append(parts, w.Kind+":"+strings.Join(w.Commands, " "))
	}
	return strings.Join(parts, "|")
}
