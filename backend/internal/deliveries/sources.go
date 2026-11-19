package deliveries

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/filter"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/shelves"
)

// The kinds of source a device follows.
const (
	SourceCollection = "collection"
	SourceSmart      = "smart"
	SourceReading    = "reading"
	SourceWishlist   = "wishlist"
)

// SourceKinds are the values of Source.Kind.
var SourceKinds = []string{SourceCollection, SourceSmart, SourceReading, SourceWishlist}

// ErrBadSource is a source that names no shelf the person may look at, or
// one of no known kind.
var ErrBadSource = errors.New("no such shelf")

// MaxSources is the most sources one device follows.
const MaxSources = 50

// Source is a collection, a smart shelf or one of the owner's lists that a
// device follows: its books are queued for the device.
type Source struct {
	ID    uuid.UUID
	Kind  string
	Shelf *uuid.UUID
	Name  string
}

// SourceQueue is the job queue a pass is asked for with.
type SourceQueue interface {
	InsertMany(ctx context.Context, args []river.JobArgs, opts jobs.InsertOpts) (int, error)
}

// Sources keeps a device stocked from what it follows (#545). A pass,
// SyncUser, is safe to repeat: it queues what is on the sources and takes
// off what only a source put there and none holds any more.
type Sources struct {
	pool    *pgxpool.Pool
	books   *catalog.Service
	shelves *shelves.Service
	log     *slog.Logger
	// Queue is set once the job runner exists.
	Queue SourceQueue
	// OnQueued hears of the books a pass queued, as Service.OnQueued does.
	OnQueued func(ctx context.Context, bookIDs []uuid.UUID)
}

// NewSources returns the Sources on the pool.
func NewSources(pool *pgxpool.Pool, books *catalog.Service, shelves *shelves.Service, log *slog.Logger) *Sources {
	return &Sources{pool: pool, books: books, shelves: shelves, log: log}
}

// Of lists the sources of one of the user's devices.
func (s *Sources) Of(ctx context.Context, userID, deviceID uuid.UUID) ([]Source, error) {
	rows, err := sqlc.New(s.pool).ListDeviceSources(ctx, sqlc.ListDeviceSourcesParams{DeviceID: deviceID, UserID: userID})
	if err != nil {
		return nil, err
	}
	out := make([]Source, len(rows))
	for i, r := range rows {
		shelf := r.CollectionID
		if shelf == nil {
			shelf = r.SmartShelfID
		}
		out[i] = Source{ID: r.ID, Kind: r.Kind, Shelf: shelf, Name: r.Name}
	}
	return out, nil
}

// Set makes the sources the device follows exactly those named (Kind and
// Shelf of each), refusing a shelf the scope may not look at with
// ErrBadSource and a device that is not the user's with ErrNoDevice, and
// asks for a pass.
func (s *Sources) Set(ctx context.Context, scope library.Scope, deviceID uuid.UUID, want []Source) error {
	if len(want) > MaxSources {
		return ErrBadSource
	}
	for _, w := range want {
		switch w.Kind {
		case SourceReading, SourceWishlist:
		case SourceCollection:
			if w.Shelf == nil {
				return ErrBadSource
			}
			if _, _, err := s.shelves.Get(ctx, scope, *w.Shelf); err != nil {
				return shelfError(err)
			}
		case SourceSmart:
			if w.Shelf == nil {
				return ErrBadSource
			}
			if _, err := s.shelves.SmartShelf(ctx, scope, *w.Shelf); err != nil {
				return shelfError(err)
			}
		default:
			return ErrBadSource
		}
	}
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		owns, err := q.OwnsDevice(ctx, sqlc.OwnsDeviceParams{ID: deviceID, UserID: scope.Viewer})
		if err != nil {
			return err
		}
		if !owns {
			return ErrNoDevice
		}
		keep := []uuid.UUID{}
		for _, w := range want {
			p := sqlc.AddDeviceSourceParams{DeviceID: deviceID, Viewer: scope.Viewer, Kind: w.Kind}
			if w.Kind == SourceCollection {
				p.CollectionID = w.Shelf
			}
			if w.Kind == SourceSmart {
				p.SmartShelfID = w.Shelf
			}
			if err := q.AddDeviceSource(ctx, p); err != nil {
				return err
			}
		}
		now, err := q.ListDeviceSources(ctx, sqlc.ListDeviceSourcesParams{DeviceID: deviceID, UserID: scope.Viewer})
		if err != nil {
			return err
		}
		for _, r := range now {
			if matches(r.Kind, r.CollectionID, r.SmartShelfID, want) {
				keep = append(keep, r.ID)
			}
		}
		return q.DeleteDeviceSourcesNotIn(ctx, sqlc.DeleteDeviceSourcesNotInParams{DeviceID: deviceID, UserID: scope.Viewer, Keep: keep})
	})
	if err != nil {
		return err
	}
	return s.Request(ctx, scope.Viewer)
}

func shelfError(err error) error {
	if errors.Is(err, shelves.ErrNotFound) {
		return ErrBadSource
	}
	return err
}

func matches(kind string, collection, smart *uuid.UUID, want []Source) bool {
	for _, w := range want {
		if w.Kind != kind {
			continue
		}
		switch kind {
		case SourceCollection:
			if collection != nil && w.Shelf != nil && *collection == *w.Shelf {
				return true
			}
		case SourceSmart:
			if smart != nil && w.Shelf != nil && *smart == *w.Shelf {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// Origin says where a delivery came from: Kind and Name of the shelf.
type Origin struct {
	Kind string
	Name string
}

// OriginsOf says which sources the deliveries among ids are on, for what
// the user may look at; a delivery sent by hand only has none.
func (s *Sources) OriginsOf(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID][]Origin, error) {
	out := map[uuid.UUID][]Origin{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := sqlc.New(s.pool).ListDeliveryOrigins(ctx, sqlc.ListDeliveryOriginsParams{Ids: ids, Viewer: userID})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DeliveryID] = append(out[r.DeliveryID], Origin{Kind: r.Kind, Name: r.Name})
	}
	return out, nil
}

// Request asks for a pass for the user's devices; the nil user is every
// user's.
func (s *Sources) Request(ctx context.Context, userID uuid.UUID) error {
	if s.Queue == nil {
		return nil
	}
	_, err := s.Queue.InsertMany(ctx, []river.JobArgs{SyncArgs{UserID: userID}}, jobs.InsertOpts{Queue: jobs.QueueDefault, Unique: true, MaxAttempts: 3})
	return err
}

// SyncAll runs a pass for every user whose devices follow something.
func (s *Sources) SyncAll(ctx context.Context) error {
	users, err := sqlc.New(s.pool).ListUsersWithDeviceSources(ctx)
	if err != nil {
		return err
	}
	var failed error
	for _, u := range users {
		if err := s.SyncUser(ctx, u); err != nil {
			s.log.Warn("stocking a user's devices", "error", err)
			failed = err
		}
	}
	return failed
}

// SyncUser works through the sources of the user's devices: each one's
// books, as the user sees them now, are queued, the sources the books left
// let go of them, and what no source holds and nobody sent by hand goes
// off the devices. A source that cannot be read this time is left as it is.
func (s *Sources) SyncUser(ctx context.Context, userID uuid.UUID) error {
	q := sqlc.New(s.pool)
	role, err := q.GetSourceUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	scope := library.Scope{Viewer: userID, SeesAll: auth.Allows(role, auth.StorageManage)}
	sources, err := q.ListUserDeviceSources(ctx, userID)
	if err != nil {
		return err
	}
	var queued []uuid.UUID
	for _, src := range sources {
		ids, err := s.booksOf(ctx, scope, src)
		if err != nil {
			s.log.Warn("reading a device's source", "kind", src.Kind, "error", err)
			continue
		}
		err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			q := sqlc.New(tx)
			if err := q.SourceDeliveries(ctx, sqlc.SourceDeliveriesParams{
				Ids: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll, DeviceID: src.DeviceID, SourceID: src.ID,
			}); err != nil {
				return err
			}
			_, err := q.SourceOrigins(ctx, sqlc.SourceOriginsParams{
				Ids: ids, Viewer: scope.Viewer, SeesAll: scope.SeesAll, DeviceID: src.DeviceID, SourceID: src.ID,
			})
			return err
		})
		if err != nil {
			return err
		}
		queued = append(queued, ids...)
	}
	if _, err := q.DropUnsourcedDeliveries(ctx, userID); err != nil {
		return err
	}
	if len(queued) > 0 && s.OnQueued != nil {
		s.OnQueued(ctx, queued)
	}
	return nil
}

func (s *Sources) booksOf(ctx context.Context, scope library.Scope, src sqlc.ListUserDeviceSourcesRow) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	var err error
	switch src.Kind {
	case SourceCollection:
		_, ids, err = s.shelves.Get(ctx, scope, *src.CollectionID)
	case SourceSmart:
		ids, err = s.shelves.SmartBookIDs(ctx, scope, *src.SmartShelfID)
	case SourceReading:
		ids, err = s.byStatus(ctx, scope, catalog.StatusReading)
	case SourceWishlist:
		ids, err = s.byStatus(ctx, scope, catalog.StatusWishlist)
	default:
		return nil, fmt.Errorf("unknown source kind %q", src.Kind)
	}
	if errors.Is(err, shelves.ErrNotFound) {
		return nil, nil
	}
	return ids, err
}

func (s *Sources) byStatus(ctx context.Context, scope library.Scope, status string) ([]uuid.UUID, error) {
	return s.books.Select(ctx, scope, nil, filter.Node{Field: catalog.FieldStatus, Op: catalog.OpIn, Values: []string{status}}, nil)
}

// SyncArgs is the job that stocks one user's devices from their sources,
// or every user's when UserID is nil.
type SyncArgs struct {
	UserID uuid.UUID `json:"userId"`
}

const syncKind = "deliveries.sync_sources"

func (SyncArgs) Kind() string { return syncKind }

// SyncWorker runs SyncArgs.
type SyncWorker struct {
	river.WorkerDefaults[SyncArgs]
	Sources *Sources
}

func (w *SyncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
	if job.Args.UserID == uuid.Nil {
		return w.Sources.SyncAll(ctx)
	}
	return w.Sources.SyncUser(ctx, job.Args.UserID)
}

// SyncPeriodic is the pass over every user, every interval.
func SyncPeriodic(interval time.Duration) jobs.Schedule {
	return jobs.Every(interval, false, SyncArgs{}, jobs.QueueDefault)
}
