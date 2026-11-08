package similar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/embed"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

var (
	// ErrNoSuchModel is a name that is none of embed.Specs.
	ErrNoSuchModel = errors.New("no such embedding model")
	// ErrModelChosen is the chosen model, which is never removed.
	ErrModelChosen = errors.New("the embedding model is the chosen one")
)

// removeBatch is how many vectors one statement deletes: a model's are
// two a book, which on a large library would hold their locks for long
// in one.
const removeBatch = 500

// Removed is what removing a model freed.
type Removed struct {
	// FileBytes are its files' and its passages' bytes on disk.
	FileBytes int64
	// Vectors is how many book vectors went.
	Vectors int
}

// RemoveModel removes the files and the vectors of the named model, which
// must not be the chosen one: its files under the model directory but
// those the chosen model shares, its book vectors in batches, and its
// passages. A model chosen again meanwhile keeps the vectors not yet
// deleted. Run it only on the embed queue, whose one worker never runs it
// beside a pass.
func (s *Service) RemoveModel(ctx context.Context, name string) (Removed, error) {
	spec, ok := embed.Specs[name]
	if !ok {
		return Removed{}, fmt.Errorf("%w: %q", ErrNoSuchModel, name)
	}
	chosen, _, err := s.config(ctx)
	if err != nil {
		return Removed{}, err
	}
	if spec.Model == chosen.Model {
		return Removed{}, fmt.Errorf("%w: %s", ErrModelChosen, name)
	}
	var out Removed
	if s.ModelDir != "" {
		if out.FileBytes, err = embed.Remove(s.ModelDir, spec, chosen); err != nil {
			return out, err
		}
	}
	if s.RemovePassages != nil {
		freed, err := s.RemovePassages(ctx, spec.Model)
		out.FileBytes += freed
		if err != nil {
			return out, err
		}
	}
	q := sqlc.New(s.pool)
	after := uuid.Nil
	for {
		books, err := q.DeleteVectorsOfModel(ctx, sqlc.DeleteVectorsOfModelParams{
			Model: spec.Model.Name, ModelVersion: Version(spec.Model), After: after, Batch: removeBatch,
		})
		if err != nil {
			return out, err
		}
		out.Vectors += len(books)
		if len(books) < removeBatch {
			break
		}
		after = slices.MaxFunc(books, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
		if now, _, err := s.config(ctx); err != nil || now.Model == spec.Model {
			return out, err
		}
	}
	if out.FileBytes > 0 || out.Vectors > 0 {
		s.log.Info("removed an embedding model no longer chosen", "model", name, "bytes", out.FileBytes, "vectors", out.Vectors)
	}
	return out, nil
}

// RemoveUnused removes every model that has not been the chosen one for
// the time the settings say (RemoveAfter). A model found unused that was
// not known to be is counted unused from now, as one used before #357 or
// left by taking the setting back to its default.
func (s *Service) RemoveUnused(ctx context.Context, now time.Time) error {
	if s.RemoveAfter == nil {
		return nil
	}
	keep, err := s.RemoveAfter(ctx)
	if err != nil {
		return err
	}
	chosen, _, err := s.config(ctx)
	if err != nil {
		return err
	}
	q := sqlc.New(s.pool)
	rows, err := q.ListUnusedModels(ctx)
	if err != nil {
		return err
	}
	since := make(map[string]time.Time, len(rows))
	for _, r := range rows {
		since[r.Name] = r.Since
	}
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(embed.Specs)) {
		spec := embed.Specs[name]
		switch t, known := since[name]; {
		case spec.Model == chosen.Model:
			if known {
				errs = append(errs, q.MarkModelChosen(ctx, name))
			}
		case !known:
			errs = append(errs, q.MarkModelUnused(ctx, name))
		case now.Sub(t) >= keep:
			_, err := s.RemoveModel(ctx, name)
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RemoveArgs is the job that removes the models no longer chosen, or the
// one it names at once.
type RemoveArgs struct {
	Model string `json:"model,omitempty"`
}

// removeKind names the job in the queue. It is stored with every job, so
// it stays.
const removeKind = "similar.remove_models"

func (RemoveArgs) Kind() string { return removeKind }

// On the embed queue, whose one worker never runs it beside a pass that
// may be using the model's files or writing its vectors.
var removeOpts = jobs.InsertOpts{Queue: jobs.QueueEmbed, Unique: true, MaxAttempts: 3}

// RequestRemoval queues the removal of the named model now; the chosen one
// is ErrModelChosen.
func (s *Service) RequestRemoval(ctx context.Context, name string) error {
	spec, ok := embed.Specs[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoSuchModel, name)
	}
	chosen, _, err := s.config(ctx)
	if err != nil {
		return err
	}
	if spec.Model == chosen.Model {
		return fmt.Errorf("%w: %s", ErrModelChosen, name)
	}
	_, err = s.Queue.Insert(ctx, RemoveArgs{Model: name}, removeOpts)
	return err
}

// RemovePeriodic is the look for models no longer chosen, also at start.
func RemovePeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, true, RemoveArgs{}, jobs.QueueEmbed)
}

// RemoveWorker runs RemoveArgs.
type RemoveWorker struct {
	river.WorkerDefaults[RemoveArgs]
	Service *Service
}

func (w *RemoveWorker) Timeout(*river.Job[RemoveArgs]) time.Duration { return 30 * time.Minute }

func (w *RemoveWorker) Work(ctx context.Context, job *river.Job[RemoveArgs]) error {
	if job.Args.Model == "" {
		return w.Service.RemoveUnused(ctx, time.Now())
	}
	_, err := w.Service.RemoveModel(ctx, job.Args.Model)
	if errors.Is(err, ErrModelChosen) || errors.Is(err, ErrNoSuchModel) {
		w.Service.log.Info("an embedding model is not removed", "error", err)
		return nil
	}
	return err
}
