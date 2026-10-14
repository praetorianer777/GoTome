// Package deliveries sends books to a person's devices (#196): a book waits
// in the device's queue, which the device reads as an OPDS feed, until the
// device downloads it.
package deliveries

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

var (
	// ErrNoDevice is returned for a device that is not the person's.
	ErrNoDevice = errors.New("no such device")
	// ErrNotFound is returned for a delivery that is not the person's, or
	// that the device has already and cannot be told to remove.
	ErrNotFound = errors.New("no such delivery")
)

// MaxPage is the most books a page of a device's books holds.
const MaxPage = 100

// Service keeps the deliveries.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service on the pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Delivery is a book sent to one of the person's devices.
type Delivery struct {
	ID          uuid.UUID
	DeviceID    uuid.UUID
	DeviceName  string
	QueuedAt    time.Time
	DeliveredAt *time.Time
}

// Queue puts the books among ids that the scope sees, and that have an
// ebook on disk, in the queue of the scope's user's device, and returns
// how many were queued; a book the device has already is queued again.
func (s *Service) Queue(ctx context.Context, scope library.Scope, deviceID uuid.UUID, ids []uuid.UUID) (int, error) {
	q := sqlc.New(s.pool)
	owns, err := q.OwnsDevice(ctx, sqlc.OwnsDeviceParams{ID: deviceID, UserID: scope.Viewer})
	if err != nil {
		return 0, err
	}
	if !owns {
		return 0, ErrNoDevice
	}
	n, err := q.QueueDeliveries(ctx, sqlc.QueueDeliveriesParams{
		Ids: ids, DeviceID: deviceID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	return int(n), err
}

// ForBook lists the book's deliveries to the user's devices, the latest
// first. The caller has checked that the user sees the book.
func (s *Service) ForBook(ctx context.Context, userID, bookID uuid.UUID) ([]Delivery, error) {
	rows, err := sqlc.New(s.pool).ListBookDeliveries(ctx, sqlc.ListBookDeliveriesParams{BookID: bookID, UserID: userID})
	if err != nil {
		return nil, err
	}
	out := make([]Delivery, len(rows))
	for i, r := range rows {
		out[i] = Delivery{ID: r.ID, DeviceID: r.DeviceID, DeviceName: r.DeviceName, QueuedAt: r.QueuedAt, DeliveredAt: r.DeliveredAt}
	}
	return out, nil
}

// TakeBack takes a book off one of the user's devices: out of its queue
// before the device has it, or off a Kobo reader, whose next sync removes
// it. Other devices keep what they downloaded whatever GOtome says.
func (s *Service) TakeBack(ctx context.Context, userID, id uuid.UUID) error {
	n, err := sqlc.New(s.pool).TakeBackDelivery(ctx, sqlc.TakeBackDeliveryParams{ID: id, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Queued lists the books waiting for the device that the scope still sees,
// the first sent first.
func (s *Service) Queued(ctx context.Context, scope library.Scope, deviceID uuid.UUID) ([]uuid.UUID, error) {
	return sqlc.New(s.pool).ListQueuedBooks(ctx, sqlc.ListQueuedBooksParams{
		DeviceID: deviceID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
}

// Delivered records that the device has downloaded the book, and reports
// whether it was waiting for it.
func (s *Service) Delivered(ctx context.Context, deviceID, bookID uuid.UUID) (bool, error) {
	n, err := sqlc.New(s.pool).MarkDelivered(ctx, sqlc.MarkDeliveredParams{DeviceID: deviceID, BookID: bookID})
	return n > 0, err
}

// OnDevice records that the device has the book, which it took itself or
// was sent; ErrNotFound for a book the scope does not see.
func (s *Service) OnDevice(ctx context.Context, scope library.Scope, deviceID, bookID uuid.UUID) error {
	n, err := sqlc.New(s.pool).RecordOnDevice(ctx, sqlc.RecordOnDeviceParams{
		DeviceID: deviceID, BookID: bookID, Viewer: scope.Viewer, SeesAll: scope.SeesAll,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// OffDevice forgets the book on the device: taken off it, or no longer to
// be sent there.
func (s *Service) OffDevice(ctx context.Context, deviceID, bookID uuid.UUID) error {
	return sqlc.New(s.pool).ForgetOnDevice(ctx, sqlc.ForgetOnDeviceParams{DeviceID: deviceID, BookID: bookID})
}

// DeviceBook is a book on one of the person's devices, or on its way there.
type DeviceBook struct {
	// ID is the delivery's, by which TakeBack takes the book off.
	ID          uuid.UUID
	BookID      uuid.UUID
	QueuedAt    time.Time
	DeliveredAt *time.Time
	// SyncedAt is when a Kobo reader's library sync last told it of the
	// book; nil for other devices, and before the first sync.
	SyncedAt *time.Time
}

// How far a book got to a device.
const (
	// StateWaiting is a book the device has yet to be told of or download.
	StateWaiting = "waiting"
	// StateSynced is a book a Kobo reader was told of but has not
	// downloaded yet.
	StateSynced = "synced"
	// StateDelivered is a book the device downloaded, or says it has.
	StateDelivered = "delivered"
)

// States are the values of State.
var States = []string{StateWaiting, StateSynced, StateDelivered}

// State is how far the book got to the device.
func (b DeviceBook) State() string {
	switch {
	case b.DeliveredAt != nil:
		return StateDelivered
	case b.SyncedAt != nil:
		return StateSynced
	default:
		return StateWaiting
	}
}

// Removable is whether TakeBack takes the book off the device.
func (b DeviceBook) Removable() bool { return b.DeliveredAt == nil || b.SyncedAt != nil }

// Cursor is where a page of a device's books ended.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// OfDevice lists the books of one of the scope's user's devices that the
// scope still sees, the last sent first, after the cursor when one is
// given, and the cursor of the next page if there is one; ErrNoDevice for
// a device that is not the user's.
func (s *Service) OfDevice(ctx context.Context, scope library.Scope, deviceID uuid.UUID, after *Cursor, limit int) ([]DeviceBook, *Cursor, error) {
	q := sqlc.New(s.pool)
	owns, err := q.OwnsDevice(ctx, sqlc.OwnsDeviceParams{ID: deviceID, UserID: scope.Viewer})
	if err != nil {
		return nil, nil, err
	}
	if !owns {
		return nil, nil, ErrNoDevice
	}
	limit = min(max(limit, 1), MaxPage)
	p := sqlc.ListDeviceBooksParams{DeviceID: deviceID, Viewer: scope.Viewer, SeesAll: scope.SeesAll, MaxRows: int32(limit + 1)}
	if after != nil {
		p.BeforeAt, p.BeforeID = &after.At, &after.ID
	}
	rows, err := q.ListDeviceBooks(ctx, p)
	if err != nil {
		return nil, nil, err
	}
	out := make([]DeviceBook, len(rows))
	for i, r := range rows {
		out[i] = DeviceBook{ID: r.ID, BookID: r.BookID, QueuedAt: r.QueuedAt, DeliveredAt: r.DeliveredAt, SyncedAt: r.SyncedAt}
	}
	if len(out) <= limit {
		return out, nil, nil
	}
	out = out[:limit]
	last := out[limit-1]
	return out, &Cursor{At: last.QueuedAt, ID: last.ID}, nil
}
