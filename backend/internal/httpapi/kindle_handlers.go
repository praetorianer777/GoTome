package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/kindle"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// kindleStatus is what the caller is told about sending to a Kindle.
type kindleStatus struct {
	// Available is whether an administrator has set up a mail server.
	Available bool `json:"available"`
	// Sender is the address the mail comes from: the caller approves it in
	// their Amazon account (Content & Devices, Preferences, Personal
	// Document Settings), or Amazon drops the mail.
	Sender string `json:"sender,omitempty"`
	// Address is the caller's Kindle address, "" before they set it.
	Address string `json:"address"`
	// MaxBytes is the largest file Amazon takes by e-mail.
	MaxBytes int64 `json:"maxBytes"`
	// Format is the one format that is sent; nothing is converted.
	Format string `json:"format"`
}

type kindleAddress struct {
	// Address is a Kindle address (name@kindle.com), or "" to remove it.
	Address string `json:"address"`
}

type kindleSkipped struct {
	BookID uuid.UUID `json:"bookId"`
	// Reason is unavailable (no EPUB the caller sees), tooLarge (over
	// maxBytes) or pending (already waiting to be sent).
	Reason string `json:"reason" enum:"unavailable,tooLarge,pending"`
}

type kindleResult struct {
	// Queued is how many books will be sent.
	Queued  int             `json:"queued"`
	Skipped []kindleSkipped `json:"skipped"`
}

type kindleSendView struct {
	ID uuid.UUID `json:"id"`
	// State is queued, sending, sent or failed.
	State string `json:"state" enum:"queued,sending,sent,failed"`
	// Message says what went wrong with a failed send.
	Message    string     `json:"message,omitempty"`
	Address    string     `json:"address"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type kindleSendList struct {
	Sends []kindleSendView `json:"sends"`
}

func (s *Server) getKindle(w http.ResponseWriter, r *http.Request) error {
	st, err := s.Kindle.Status(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, kindleStatus{
		Available: st.Available, Sender: st.Sender, Address: st.Address, MaxBytes: kindle.MaxAttachment, Format: kindle.Format,
	})
	return nil
}

func (s *Server) putKindle(w http.ResponseWriter, r *http.Request) error {
	var req kindleAddress
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err := s.Kindle.SetAddress(r.Context(), UserFrom(r.Context()).ID, req.Address)
	if errors.Is(err, kindle.ErrBadAddress) {
		return ErrValidation(map[string]string{"address": "Give the address of your Kindle, such as name@kindle.com."})
	}
	if err != nil {
		return err
	}
	return s.getKindle(w, r)
}

func (s *Server) sendToKindle(w http.ResponseWriter, r *http.Request) error {
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
	res, err := s.Kindle.Request(r.Context(), scope, books)
	switch {
	case errors.Is(err, kindle.ErrNotConfigured):
		return ErrConflict("Sending to a Kindle is not set up on this server. Ask an administrator to add a mail server.")
	case errors.Is(err, kindle.ErrNoAddress):
		return ErrConflict("Add your Kindle address on your profile first.")
	case err != nil:
		return err
	}
	out := kindleResult{Queued: res.Queued, Skipped: make([]kindleSkipped, len(res.Skipped))}
	for i, k := range res.Skipped {
		out.Skipped[i] = kindleSkipped{BookID: k.BookID, Reason: k.Reason}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) listBookKindleSends(w http.ResponseWriter, r *http.Request) error {
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
	list, err := s.Kindle.ForBook(r.Context(), user.ID, id)
	if err != nil {
		return err
	}
	out := kindleSendList{Sends: make([]kindleSendView, len(list))}
	for i, k := range list {
		out.Sends[i] = kindleSendView{ID: k.ID, State: k.State, Message: k.Message, Address: k.Address, CreatedAt: k.CreatedAt, FinishedAt: k.FinishedAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
