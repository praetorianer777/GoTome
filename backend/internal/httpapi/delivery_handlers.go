package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

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

func (s *Server) takeBackDelivery(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "deliveryId", "delivery")
	if err != nil {
		return err
	}
	err = s.Deliveries.TakeBack(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, deliveries.ErrNotFound) {
		return ErrNotFound("There is no such delivery waiting.")
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
