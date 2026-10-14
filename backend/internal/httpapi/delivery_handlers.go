package httpapi

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/deliveries"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/opds"
)

type sendRequest struct {
	Books   []uuid.UUID `json:"books,omitempty"`
	Library string      `json:"library,omitempty"`
	Filter  string      `json:"filter,omitempty"`
}

type sendResult struct {
	// Queued is how many books now wait for the device.
	Queued int `json:"queued"`
}

type deliveryView struct {
	ID          uuid.UUID  `json:"id"`
	DeviceID    uuid.UUID  `json:"deviceId"`
	DeviceName  string     `json:"deviceName"`
	QueuedAt    time.Time  `json:"queuedAt"`
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
}

type deliveryList struct {
	Deliveries []deliveryView `json:"deliveries"`
}

// deviceBook is a book on one of the caller's devices, or on its way there.
type deviceBook struct {
	Book bookSummary `json:"book"`
	// DeliveryID takes the book off the device (DELETE /deliveries/{id})
	// where Removable says it can.
	DeliveryID uuid.UUID `json:"deliveryId"`
	// State is waiting until the device has downloaded the book, synced
	// while a Kobo reader was told of it but has not downloaded it, and
	// delivered once it has.
	State    string    `json:"state"`
	QueuedAt time.Time `json:"queuedAt"`
	// DeliveredAt is when the device downloaded the book, or said it has it.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// SyncedAt is when a Kobo reader's sync last told it of the book.
	SyncedAt *time.Time `json:"syncedAt,omitempty"`
	// Removable is whether taking the book off reaches the device: while
	// it waits, or from a Kobo reader at its next sync. Other devices keep
	// what they downloaded.
	Removable bool `json:"removable"`
}

type deviceBookList struct {
	Device deviceView   `json:"device"`
	Books  []deviceBook `json:"books"`
	// NextCursor asks for the books sent before; left out on the last page.
	NextCursor string `json:"nextCursor,omitempty"`
}

type deviceBooksQuery struct {
	Cursor string `query:"cursor" doc:"The nextCursor of the page before."`
	Limit  int    `query:"limit" doc:"How many books a page holds, at most 100; 50 when left out."`
}

func (s *Server) sendToDevice(w http.ResponseWriter, r *http.Request) error {
	device, err := pathID(r, "deviceId", "device")
	if err != nil {
		return err
	}
	var req sendRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	tooMany := ErrValidation(map[string]string{"books": fmt.Sprintf("At most %d books are sent at once; choose fewer.", catalog.MaxSelection)})
	if len(req.Books) > catalog.MaxSelection {
		return tooMany
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	books := req.Books
	if len(books) == 0 {
		libraryID, tree, err := bookSelection(req.Library, req.Filter)
		if err != nil {
			return err
		}
		books, err = s.Books.Select(r.Context(), scope, libraryID, tree, nil)
		switch {
		case errors.Is(err, catalog.ErrTooMany):
			return tooMany
		case catalog.IsFilterError(err):
			return ErrValidation(map[string]string{"filter": err.Error()})
		case err != nil:
			return err
		}
	}
	n, err := s.Deliveries.Queue(r.Context(), scope, device, books)
	if errors.Is(err, deliveries.ErrNoDevice) {
		return ErrNotFound("There is no such device.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, sendResult{Queued: n})
	return nil
}

func (s *Server) listBookDeliveries(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	user := UserFrom(r.Context())
	_, err = s.Books.Get(r.Context(), library.ScopeOf(*user), id)
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	list, err := s.Deliveries.ForBook(r.Context(), user.ID, id)
	if err != nil {
		return err
	}
	out := deliveryList{Deliveries: make([]deliveryView, len(list))}
	for i, d := range list {
		out.Deliveries[i] = deliveryView{ID: d.ID, DeviceID: d.DeviceID, DeviceName: d.DeviceName, QueuedAt: d.QueuedAt, DeliveredAt: d.DeliveredAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// listDeviceBooks lists the books of one of the caller's devices, the last
// sent first, each with how far it got.
func (s *Server) listDeviceBooks(w http.ResponseWriter, r *http.Request) error {
	deviceID, err := pathID(r, "deviceId", "device")
	if err != nil {
		return err
	}
	var q deviceBooksQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	var after *deliveries.Cursor
	if q.Cursor != "" {
		at, id, err := decodeTimeCursor(q.Cursor)
		if err != nil {
			return errForeignCursor()
		}
		after = &deliveries.Cursor{At: at, ID: id}
	}
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	list, next, err := s.Deliveries.OfDevice(r.Context(), scope, deviceID, after, cmp.Or(q.Limit, 50))
	if errors.Is(err, deliveries.ErrNoDevice) {
		return ErrNotFound("There is no such device.")
	}
	if err != nil {
		return err
	}
	devices, err := s.Auth.Devices(r.Context(), user.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(devices, func(d auth.Device) bool { return d.ID == deviceID })
	if i < 0 {
		return ErrNotFound("There is no such device.")
	}
	ids := make([]uuid.UUID, len(list))
	for i, b := range list {
		ids[i] = b.BookID
	}
	summaries, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]catalog.Summary, len(summaries))
	for _, b := range summaries {
		byID[b.ID] = b
	}
	out := deviceBookList{Device: viewOf(devices[i]), Books: make([]deviceBook, 0, len(list))}
	for _, b := range list {
		summary, ok := byID[b.BookID]
		if !ok {
			continue
		}
		out.Books = append(out.Books, deviceBook{
			Book: summaryOf(summary), DeliveryID: b.ID, State: b.State(), QueuedAt: b.QueuedAt,
			DeliveredAt: b.DeliveredAt, SyncedAt: b.SyncedAt, Removable: b.Removable(),
		})
	}
	if next != nil {
		out.NextCursor = encodeTimeCursor(next.At, next.ID)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) takeBackDelivery(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "deliveryId", "delivery")
	if err != nil {
		return err
	}
	err = s.Deliveries.TakeBack(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, deliveries.ErrNotFound) {
		return ErrNotFound("There is no such delivery that can be taken back.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// errNotADevice answers the routes only a device signs in to.
func errNotADevice() error {
	return ErrNotFound("Only a device signed in with its token has books sent to it.")
}

// opdsDevice lists the books sent to the device that asks, each with
// downloads that mark it delivered.
func (s *Server) opdsDevice(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	ids, err := s.Deliveries.Queued(r.Context(), scope, device)
	if err != nil {
		return err
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	f := opdsFeed(r, "Sent to this device", opds.AcquisitionType, opdsStarted)
	if f.Entries, err = s.opdsEntries(r.Context(), scope, books); err != nil {
		return err
	}
	for i := range f.Entries {
		for j, l := range f.Entries[i].Links {
			if l.Rel == opds.RelAcquisition {
				f.Entries[i].Links[j].Href = opdsDeviceDownload(l.Href)
			}
		}
	}
	return writeFeed(w, opds.AcquisitionType, f)
}

// opdsDeviceDownload is the device's own address of a file's download.
func opdsDeviceDownload(href string) string {
	var id string
	if _, err := fmt.Sscanf(href, APIPrefix+"/files/%36s", &id); err != nil {
		return href
	}
	return opdsPath + "/device/files/" + id
}

// opdsDeviceFile sends a file to the device that asks and, once the whole
// of it has gone out, marks its book delivered there.
func (s *Server) opdsDeviceFile(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	book, err := s.serveFile(w, r)
	if err != nil {
		return err
	}
	// A part of the file, or one the device already has, is not a delivery.
	rec, ok := w.(*statusRecorder)
	if !ok || rec.status != http.StatusOK || r.Method != http.MethodGet || r.Context().Err() != nil {
		return nil
	}
	if _, err := s.Deliveries.Delivered(r.Context(), device, book); err != nil {
		loggerFrom(r.Context()).Error("marking a delivery", "error", err)
	}
	return nil
}

type thisDevice struct {
	Device   deviceView `json:"device"`
	Username string     `json:"username"`
}

// getThisDevice tells a device's app whom its token signs in.
func (s *Server) getThisDevice(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	user := UserFrom(r.Context())
	devices, err := s.Auth.Devices(r.Context(), user.ID)
	if err != nil {
		return err
	}
	for _, d := range devices {
		if d.ID == device {
			writeJSON(w, r, http.StatusOK, thisDevice{Device: viewOf(d), Username: user.Username})
			return nil
		}
	}
	return errNotADevice()
}

// listDeviceQueue lists the books sent to the device that asks, as the
// OPDS feed does, for a device's own app.
func (s *Server) listDeviceQueue(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	ids, err := s.Deliveries.Queued(r.Context(), scope, device)
	if err != nil {
		return err
	}
	books, err := s.Books.Summaries(r.Context(), scope, ids)
	if err != nil {
		return err
	}
	out := bookList{Books: make([]bookSummary, len(books))}
	for i, b := range books {
		out.Books[i] = summaryOf(b)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// putOnDevice records that the device that asks has the book, so that its
// page says so.
func (s *Server) putOnDevice(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	err = s.Deliveries.OnDevice(r.Context(), library.ScopeOf(*UserFrom(r.Context())), device, id)
	if errors.Is(err, deliveries.ErrNotFound) {
		return ErrNotFound("There is no such book.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// takeOffDevice records that the device that asks no longer has the book,
// nor waits for it.
func (s *Server) takeOffDevice(w http.ResponseWriter, r *http.Request) error {
	device := DeviceFrom(r.Context())
	if device == uuid.Nil {
		return errNotADevice()
	}
	id, err := pathID(r, "bookId", "book")
	if err != nil {
		return err
	}
	if err := s.Deliveries.OffDevice(r.Context(), device, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
