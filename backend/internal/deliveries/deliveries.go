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
	// that the device has already.
	ErrNotFound = errors.New("no such delivery")
)

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

// TakeBack takes a book out of the queue of one of the user's devices
// before the device has it.
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
