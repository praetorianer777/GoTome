package ingest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

// MeasureArgs is the job that measures the EPUBs read before measuring
// was, and those whose bytes changed since (#233).
type MeasureArgs struct{}

const measureKind = "ingest.measure_files"

func (MeasureArgs) Kind() string { return measureKind }

// measureBatch is how many files a pass measures at most. Each costs its
// archive's directory, its package document and the headers of its
// images, a few hundred kilobytes at most.
const measureBatch = 200

// MeasureWorker runs MeasureArgs.
type MeasureWorker struct {
	river.WorkerDefaults[MeasureArgs]
	Service *Service
}

func (w *MeasureWorker) Work(ctx context.Context, _ *river.Job[MeasureArgs]) error {
	n, err := w.Service.MeasureFiles(ctx)
	if err != nil {
		return err
	}
	if n == measureBatch {
		return river.JobSnooze(time.Second)
	}
	return nil
}

// MeasurePeriodic is the measuring pass, every interval and at start.
func MeasurePeriodic(interval time.Duration) *river.PeriodicJob {
	return jobs.Every(interval, true, MeasureArgs{}, jobs.QueueDefault)
}

// MeasureFiles measures a batch of EPUBs not measured yet and returns how
// many it took. A file that cannot be measured is recorded without
// findings, until its bytes change.
func (s *Service) MeasureFiles(ctx context.Context) (int, error) {
	q := sqlc.New(s.pool)
	files, err := q.ListFilesToMeasure(ctx, measureBatch)
	if err != nil {
		return 0, err
	}
	for _, f := range files {
		m, err := measureEPUB(filepath.Join(f.RootPath, filepath.FromSlash(f.RelPath)))
		if err != nil {
			s.log.Debug("measuring an EPUB", "file", f.ID, "error", err)
		}
		if err := setMeasures(ctx, q, f.ID, f.Sha256, m); err != nil {
			return 0, err
		}
	}
	return len(files), nil
}

func measureEPUB(path string) (*epub.Measures, error) {
	f, size, err := openFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := epub.Measure(f, size)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// setMeasures records a file's measures and findings; nil is a file that
// could not be measured.
func setMeasures(ctx context.Context, q *sqlc.Queries, fileID uuid.UUID, sha256 []byte, m *epub.Measures) error {
	params := sqlc.SetFileMeasuresParams{ID: fileID, Sha256: sha256, Findings: []string{}}
	if m != nil {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		params.Measures, params.Findings = raw, m.Findings()
	}
	return q.SetFileMeasures(ctx, params)
}
