package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/pairing"
)

type deviceRequestCreate struct {
	// Username is the account the device asks to be added to.
	Username string `json:"username"`
	// DeviceName is what the device is called on the profile once added.
	DeviceName string `json:"deviceName"`
}

// deviceTicket is the device's answer to its request, whatever the user name.
type deviceTicket struct {
	ID uuid.UUID `json:"id"`
	// Secret is what the device asks about its request with; nobody else
	// learns it.
	Secret string `json:"secret"`
	// Code is what the device shows, for the person to compare with the one
	// on their profile before accepting.
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type deviceCollect struct {
	Secret string `json:"secret"`
}

type deviceAnswer struct {
	// State is pending, accepted, declined or expired: a request whose time
	// is up or whose token was collected already.
	State string `json:"state"`
	// Username and Token, once accepted, are what the device signs in with
	// from then on. The token is sent this once.
	Username string `json:"username,omitempty"`
	Token    string `json:"token,omitempty"`
}

type deviceRequestView struct {
	ID         uuid.UUID `json:"id"`
	DeviceName string    `json:"deviceName"`
	// Code is what the asking device shows.
	Code string `json:"code"`
	// Address is where the request came from.
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type deviceRequestList struct {
	Requests []deviceRequestView `json:"requests"`
}

func (s *Server) requestDevice(w http.ResponseWriter, r *http.Request) error {
	var req deviceRequestCreate
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	address := clientAddress(r)
	if wait := s.Logins.devices.RetryAfter(address); wait > 0 {
		return ErrTooManyRequests("Too many devices asked to be added. Wait a little and try again.", wait)
	}
	s.Logins.devices.Fail(address)
	ticket, err := s.Pairing.Request(r.Context(), req.Username, req.DeviceName, address)
	var invalid *auth.ValidationError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid.Fields)
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusCreated, deviceTicket{ID: ticket.ID, Secret: ticket.Secret, Code: ticket.Code, ExpiresAt: ticket.ExpiresAt})
	return nil
}

func (s *Server) collectDeviceToken(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "requestId", "request")
	if err != nil {
		return err
	}
	var req deviceCollect
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	answer, err := s.Pairing.Collect(r.Context(), id, req.Secret)
	if errors.Is(err, pairing.ErrNotFound) {
		return ErrNotFound("There is no such request.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, deviceAnswer{State: answer.State, Username: answer.Username, Token: answer.Token})
	return nil
}

func (s *Server) listDeviceRequests(w http.ResponseWriter, r *http.Request) error {
	waiting, err := s.Pairing.Waiting(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := deviceRequestList{Requests: make([]deviceRequestView, len(waiting))}
	for i, q := range waiting {
		out.Requests[i] = deviceRequestView{ID: q.ID, DeviceName: q.DeviceName, Code: q.Code, Address: q.Address, CreatedAt: q.CreatedAt, ExpiresAt: q.ExpiresAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) acceptDeviceRequest(w http.ResponseWriter, r *http.Request) error {
	return s.decideDeviceRequest(w, r, true)
}

func (s *Server) declineDeviceRequest(w http.ResponseWriter, r *http.Request) error {
	return s.decideDeviceRequest(w, r, false)
}

func (s *Server) decideDeviceRequest(w http.ResponseWriter, r *http.Request, accept bool) error {
	id, err := pathID(r, "requestId", "request")
	if err != nil {
		return err
	}
	err = s.Pairing.Decide(r.Context(), UserFrom(r.Context()).ID, id, accept)
	if errors.Is(err, pairing.ErrNotFound) {
		return ErrNotFound("There is no such request waiting.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
