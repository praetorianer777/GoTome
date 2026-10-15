package resources

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

const (
	// CheckEvery is how often Watch checks: a disk fills faster than
	// memory settings change.
	CheckEvery = 10 * time.Minute
	// RepeatEvery is how often warnings that stand are logged again.
	RepeatEvery = time.Hour
)

// messages are what the log says for each kind of warning.
var messages = map[string]string{
	KindMemory:    "Postgres's memory settings, with the app's and the embedding model's, exceed this machine's memory; lower them with the commands given, then restart the database",
	KindEmbedding: "the embedding model alone takes nearly all of this machine's memory; choose a smaller one, run it on an embedding server, or turn embedding off",
	KindParallel:  "Postgres may start more parallel workers than this machine has CPUs; lower them with the commands given",
	KindDisk:      "the data directory's disk is nearly full; Postgres stops when its disk is full. Free space, for instance with docker image prune after an update",
}

// logState decides when the warnings are logged: at the first check,
// whenever they change, and again every RepeatEvery while any stand.
type logState struct {
	checked bool
	last    string
	logged  time.Time
}

func (l *logState) due(key string, now time.Time) bool {
	if !l.checked || key != l.last || (key != "" && now.Sub(l.logged) >= RepeatEvery) {
		l.checked, l.last, l.logged = true, key, now
		return true
	}
	return false
}

// Watch checks every CheckEvery until ctx ends and logs what it finds, as
// logState decides.
func (s *Service) Watch(ctx context.Context, log *slog.Logger) {
	var state logState
	for {
		h, err := s.Check(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			if state.due("error:"+err.Error(), time.Now()) {
				log.Warn("could not check the machine's resources", "error", err)
			}
		case state.due(h.key(), time.Now()):
			logHealth(log, h)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(CheckEvery):
		}
	}
}

func logHealth(log *slog.Logger, h Health) {
	m, pg, d := h.Memory, h.Postgres, h.Disk
	figures := []any{
		"memory", m.MachineBytes, "containerLimit", m.Limited, "cpus", m.CPUs,
		"postgres", pg.NeedBytes, "app", m.AppBytes, "embeddingModel", m.EmbeddingModel, "embedding", m.EmbeddingBytes,
		"sharedBuffers", pg.SharedBuffersBytes, "maintenanceWorkMem", pg.MaintenanceWorkMemBytes, "workMem", pg.WorkMemBytes,
		"maxParallelWorkers", pg.MaxParallelWorkers,
		"disk", d.Path, "free", d.FreeBytes, "total", d.TotalBytes, "database", d.DatabaseBytes, "index", d.IndexBytes,
	}
	if len(h.Warnings) == 0 {
		log.Info("the machine has room for Postgres, the app and the embedding model", figures...)
		return
	}
	for _, w := range h.Warnings {
		attrs := append([]any{"kind", w.Kind}, figures...)
		if len(w.Commands) > 0 {
			attrs = append(attrs, "commands", strings.Join(w.Commands, " "))
		}
		log.Warn(messages[w.Kind], attrs...)
	}
}
