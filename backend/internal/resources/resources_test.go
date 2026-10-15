package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const (
	mb = int64(1) << 20
	gb = int64(1) << 30
)

func kinds(h Health) []string {
	var out []string
	for _, w := range h.Warnings {
		out = append(out, w.Kind)
	}
	return out
}

func commands(h Health, kind string) []string {
	for _, w := range h.Warnings {
		if w.Kind == kind {
			return w.Commands
		}
	}
	return nil
}

// defaults is Postgres as it starts without tuning, on a machine of the
// given memory with four CPUs and plenty of disk.
func defaults(machine int64) Health {
	return Health{
		Memory: Memory{MachineBytes: machine, CPUs: 4, AppBytes: AppBytes},
		Postgres: Postgres{
			SharedBuffersBytes: 128 * mb, WorkMemBytes: 4 * mb, MaintenanceWorkMemBytes: 64 * mb,
			EffectiveCacheSizeBytes: 4 * gb, MaxConnections: 100, Connections: 16,
			MaxParallelWorkers: 4, MaxParallelWorkersPerGather: 2, MaxParallelMaintenanceWorkers: 2,
		},
		Disk: Disk{FreeBytes: 500 * gb, TotalBytes: 1000 * gb, DatabaseBytes: 2 * gb},
	}
}

func TestPostgresDefaultsFitASmallMachine(t *testing.T) {
	h := Evaluate(defaults(2 * gb))
	if len(h.Warnings) != 0 {
		t.Fatalf("warnings %v", h.Warnings)
	}
	if want := 128*mb + 64*mb + 16*4*mb; h.Postgres.NeedBytes != want {
		t.Errorf("Postgres needs %d, want %d", h.Postgres.NeedBytes, want)
	}
}

// The owner's installation (#240): ParadeDB sized Postgres for a large
// machine, and the volume moved to one of 5 GB.
func TestSettingsOfABigMachineOnASmallOneWarnWithValuesThatFit(t *testing.T) {
	h := defaults(5 * gb)
	h.Postgres.SharedBuffersBytes = 7987 * mb
	h.Postgres.MaintenanceWorkMemBytes = 2 * gb
	h.Postgres.WorkMemBytes = 32 * mb
	h.Postgres.EffectiveCacheSizeBytes = 24 * gb
	h.Postgres.MaxParallelWorkers, h.Postgres.MaxParallelWorkersPerGather, h.Postgres.MaxParallelMaintenanceWorkers = 60, 30, 30
	h = Evaluate(h)
	if got := kinds(h); !slices.Equal(got, []string{KindMemory, KindParallel}) {
		t.Fatalf("warnings %v", got)
	}
	want := []string{
		"ALTER SYSTEM SET shared_buffers = '1024MB';",
		"ALTER SYSTEM SET maintenance_work_mem = '256MB';",
		"ALTER SYSTEM SET work_mem = '16MB';",
		"ALTER SYSTEM SET effective_cache_size = '2560MB';",
	}
	if got := commands(h, KindMemory); !slices.Equal(got, want) {
		t.Errorf("memory commands\n%v\nwant\n%v", got, want)
	}
	want = []string{
		"ALTER SYSTEM SET max_parallel_workers = 4;",
		"ALTER SYSTEM SET max_parallel_workers_per_gather = 2;",
		"ALTER SYSTEM SET max_parallel_maintenance_workers = 2;",
	}
	if got := commands(h, KindParallel); !slices.Equal(got, want) {
		t.Errorf("parallel commands\n%v\nwant\n%v", got, want)
	}
}

// The suggested values, applied, fit: the check does not warn again.
func TestTheSuggestedValuesFit(t *testing.T) {
	for _, c := range []struct {
		machine, model int64
	}{{1 * gb, 0}, {5 * gb, 0}, {5 * gb, 3 * gb}, {8 * gb, 1100 * mb}, {64 * gb, 3 * gb}} {
		h := defaults(c.machine)
		h.Memory.EmbeddingBytes = c.model
		h.Postgres.SharedBuffersBytes = c.machine
		h.Postgres.MaintenanceWorkMemBytes = c.machine / 2
		h.Postgres.WorkMemBytes = c.machine / 10
		h = Evaluate(h)
		if !slices.Contains(kinds(h), KindMemory) {
			t.Fatalf("%d MB, model %d MB: no memory warning", c.machine/mb, c.model/mb)
		}
		applied := h
		for _, cmd := range commands(h, KindMemory) {
			var name string
			var v int64
			if _, err := fmt.Sscanf(cmd, "ALTER SYSTEM SET %s = '%dMB';", &name, &v); err != nil {
				t.Fatal(cmd, err)
			}
			switch name {
			case "shared_buffers":
				applied.Postgres.SharedBuffersBytes = v * mb
			case "maintenance_work_mem":
				applied.Postgres.MaintenanceWorkMemBytes = v * mb
			case "work_mem":
				applied.Postgres.WorkMemBytes = v * mb
			}
		}
		applied = Evaluate(applied)
		if slices.Contains(kinds(applied), KindMemory) && !slices.Contains(kinds(applied), KindEmbedding) {
			t.Errorf("%d MB, model %d MB: the suggested values need %d MB of %d", c.machine/mb, c.model/mb,
				applied.Memory.NeedBytes/mb, applied.Memory.BudgetBytes/mb)
		}
	}
}

func TestAModelTooLargeForTheMachineIsSaid(t *testing.T) {
	h := defaults(4 * gb)
	h.Memory.EmbeddingBytes = 3 * gb
	if got := kinds(Evaluate(h)); !slices.Contains(got, KindEmbedding) {
		t.Errorf("bge-m3 on 4 GB: warnings %v", got)
	}
	h = defaults(5 * gb)
	h.Memory.EmbeddingBytes = 3 * gb
	if got := kinds(Evaluate(h)); len(got) != 0 {
		t.Errorf("bge-m3 on 5 GB with Postgres's defaults: warnings %v", got)
	}
}

func TestAnUnknownMachineIsNoWarning(t *testing.T) {
	h := defaults(0)
	h.Postgres.SharedBuffersBytes = 100 * gb
	h.Memory.CPUs = 0
	h.Postgres.MaxParallelWorkers = 60
	h.Disk = Disk{}
	if got := kinds(Evaluate(h)); len(got) != 0 {
		t.Errorf("warnings %v", got)
	}
}

func TestADiskNearlyFullWarns(t *testing.T) {
	for name, c := range map[string]struct {
		free, total, database, index int64
		warn                         bool
	}{
		"plenty":                   {500 * gb, 1000 * gb, 2 * gb, 1 * gb, false},
		"under 10 GB":              {9 * gb, 50 * gb, 1 * gb, 0, true},
		"under a tenth":            {90 * gb, 1000 * gb, 1 * gb, 0, true},
		"no room for the database": {30 * gb, 100 * gb, 25 * gb, 6 * gb, true},
		"a full disk":              {0, 110 * gb, 20 * gb, 3 * gb, true},
	} {
		h := defaults(8 * gb)
		h.Disk = Disk{FreeBytes: c.free, TotalBytes: c.total, DatabaseBytes: c.database, IndexBytes: c.index}
		if got := slices.Contains(kinds(Evaluate(h)), KindDisk); got != c.warn {
			t.Errorf("%s: warned %v", name, got)
		}
	}
}

func TestMemTotalIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal:        5242880 kB\nMemFree:          100000 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := memTotal(path); got != 5*gb {
		t.Errorf("MemTotal %d", got)
	}
	if got := memTotal(filepath.Join(t.TempDir(), "missing")); got != 0 {
		t.Errorf("missing file: %d", got)
	}
}

func TestWarningsAreLoggedWhenTheyChangeAndHourly(t *testing.T) {
	var l logState
	start := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	steps := []struct {
		key   string
		after time.Duration
		want  bool
	}{
		{"", 0, true},
		{"", CheckEvery, false},
		{"", 2 * RepeatEvery, false},
		{"disk:", 2*RepeatEvery + CheckEvery, true},
		{"disk:", 2*RepeatEvery + 2*CheckEvery, false},
		{"disk:", 3*RepeatEvery + CheckEvery, true},
		{"disk:|memory:x", 3*RepeatEvery + 2*CheckEvery, true},
		{"", 3*RepeatEvery + 3*CheckEvery, true},
	}
	for i, s := range steps {
		if got := l.due(s.key, start.Add(s.after)); got != s.want {
			t.Errorf("step %d (%q): due %v", i, s.key, got)
		}
	}
}
