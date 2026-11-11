package main

import (
	"context"
	"time"

	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/autocurate"
	"github.com/praetorianer777/gotome/backend/internal/autodupe"
	"github.com/praetorianer777/gotome/backend/internal/bulk"
	"github.com/praetorianer777/gotome/backend/internal/curation"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/fulltext"
	"github.com/praetorianer777/gotome/backend/internal/genres"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/kindle"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
	"github.com/praetorianer777/gotome/backend/internal/notify"
	"github.com/praetorianer777/gotome/backend/internal/passages"
	"github.com/praetorianer777/gotome/backend/internal/readimport"
	"github.com/praetorianer777/gotome/backend/internal/releases"
	"github.com/praetorianer777/gotome/backend/internal/search"
	"github.com/praetorianer777/gotome/backend/internal/similar"
	"github.com/praetorianer777/gotome/backend/internal/updates"
)

// background is what the workers of serve work with.
type background struct {
	accounts    *auth.Service
	matches     *enrich.Service
	changes     *bulk.Service
	scans       *ingest.Service
	searchIndex *search.Index
	textIndex   *fulltext.Service
	duplicates  *dedupe.Service
	curations   *curation.Service
	autoDupes   *autodupe.Service
	vectors     *similar.Service
	genres      *genres.Service
	passages    *passages.Service
	events      *notify.Events
	follows     *releases.Service
	sync        *kosync.Service
	updates     *updates.Service
	imports     *readimport.Service
	kindle      *kindle.Service
	automatic   *autocurate.Service
	// trashRetention is how long the trash keeps a file, as the settings say.
	trashRetention func(context.Context) (time.Duration, error)
}

// registration is one worker, with the kind of job it works.
type registration struct {
	args river.JobArgs
	add  func(*jobs.Workers)
}

func worker[T river.JobArgs](w river.Worker[T]) registration {
	var args T
	return registration{args: args, add: func(workers *jobs.Workers) { river.AddWorker(workers, w) }}
}

// registrations are every worker serve runs. A test holds them to every
// job kind there is, so a job that is queued is also worked.
func (b background) registrations() []registration {
	return []registration{
		worker(&enrich.MatchWorker{Service: b.matches}),
		worker(&bulk.Worker{Service: b.changes}),
		worker(&autocurate.CleanupWorker{Service: b.automatic}),
		worker(&autocurate.MatchesWorker{Service: b.automatic}),
		worker(&autocurate.DailyWorker{Service: b.automatic}),
		worker(&readimport.Worker{Service: b.imports}),
		worker(&auth.SweepSessionsWorker{Service: b.accounts}),
		worker(&ingest.ScanWorker{Service: b.scans}),
		worker(&ingest.ScanAllWorker{Service: b.scans}),
		worker(&ingest.ExtractWorker{Service: b.scans}),
		worker(&ingest.ChunkWorker{Service: b.scans}),
		worker(&ingest.WriteBackWorker{Service: b.scans}),
		worker(&search.RebuildIndexWorker{Index: b.searchIndex}),
		worker(&fulltext.ApplyWorker{Service: b.textIndex}),
		worker(&ingest.IndexFilesWorker{Service: b.scans}),
		worker(&ingest.JoinWorker{Service: b.scans}),
		worker(&ingest.JoinPlaceholdersWorker{Service: b.scans}),
		worker(&dedupe.CheckWorker{Service: b.duplicates}),
		worker(&dedupe.SignWorker{Service: b.duplicates}),
		worker(&autodupe.RunWorker{Service: b.autoDupes}),
		worker(&autodupe.DailyWorker{Service: b.autoDupes}),
		worker(&curation.UndoWorker{Service: b.curations}),
		worker(&similar.EmbedWorker{Service: b.vectors}),
		worker(&similar.RemoveWorker{Service: b.vectors}),
		worker(&genres.SuggestWorker{Service: b.genres}),
		worker(&passages.EmbedWorker{Service: b.passages}),
		worker(&notify.FlushWorker{Events: b.events}),
		worker(&releases.PollWorker{Service: b.follows}),
		worker(&releases.FetchFeedsWorker{Service: b.follows}),
		worker(&updates.CheckWorker{Service: b.updates}),
		worker(&kosync.HashWorker{Service: b.sync}),
		worker(&kindle.Worker{Service: b.kindle}),
		worker(&ingest.MeasureWorker{Service: b.scans}),
		worker(&ingest.PurgeTrashWorker{Service: b.scans, Retention: b.trashRetention}),
	}
}

func (b background) workers() *jobs.Workers {
	workers := jobs.NewWorkers()
	for _, r := range b.registrations() {
		r.add(workers)
	}
	return workers
}

// schedules are the jobs serve enqueues at intervals. Scans are scheduled
// only when scanInterval is set.
func schedules(scanInterval time.Duration) []jobs.Schedule {
	periodic := []jobs.Schedule{
		jobs.Every(sessionSweepInterval, true, auth.SweepSessionsArgs{}, jobs.QueueDefault),
		jobs.Every(trashPurgeInterval, true, ingest.PurgeTrashArgs{}, jobs.QueueDefault),
		jobs.Every(autodupe.DailyInterval, true, autodupe.DailyArgs{}, jobs.QueueDefault),
		similar.Periodic(embedInterval),
		similar.RemovePeriodic(removeModelsInterval),
		genres.Periodic(genreInterval),
		passages.Periodic(embedInterval),
		releases.Periodic(releasePollInterval),
		releases.FeedsPeriodic(feedFetchInterval),
		updates.Periodic(updateCheckInterval),
		kosync.Periodic(koreaderHashInterval),
		ingest.MeasurePeriodic(measureInterval),
		fulltext.Periodic(outboxInterval),
		autocurate.Periodic(curationInterval),
	}
	if scanInterval > 0 {
		// Also once at start: what changed on disk while GOtome was down is
		// found now, and an unchanged library costs a walk.
		periodic = append(periodic, jobs.Every(scanInterval, true, ingest.ScanAllArgs{}, jobs.QueueDefault))
	}
	return periodic
}
