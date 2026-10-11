package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/kosync"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/reading"
)

// kosyncPath is where KOReader's progress sync finds its server: the
// address a person enters in KOReader as the custom sync server.
const kosyncPath = APIPrefix + "/kosync"

type kosyncAuthorized struct {
	Authorized string `json:"authorized"`
}

type kosyncProgress struct {
	Document   string  `json:"document"`
	Progress   string  `json:"progress"`
	Percentage float64 `json:"percentage"`
	Device     string  `json:"device"`
	DeviceID   string  `json:"device_id"`
}

type kosyncPosition struct {
	Document   string  `json:"document,omitempty"`
	Progress   string  `json:"progress,omitempty"`
	Percentage float64 `json:"percentage,omitempty"`
	Device     string  `json:"device,omitempty"`
	DeviceID   string  `json:"device_id,omitempty"`
	// Timestamp is when the place was saved, in seconds since 1970.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type kosyncSaved struct {
	Document  string `json:"document"`
	Timestamp int64  `json:"timestamp"`
}

func (s *Server) kosyncRoutes() []Route {
	return []Route{
		{
			Method: http.MethodGet, Path: "/kosync/users/auth", ID: "kosyncAuth",
			Summary: "KOReader's progress sync checks its sign-in: the user name in x-auth-user, the MD5 of a device token in x-auth-key", Tag: "kosync",
			Response: kosyncAuthorized{}, Reads: ReadsNoLibrary, Permission: auth.SignedIn, Devices: true, Handler: s.kosyncAuth,
		},
		{
			Method: http.MethodPost, Path: "/kosync/users/create", ID: "kosyncRegister",
			Summary: "KOReader's progress sync registers an account: refused, as accounts and devices are made in GOtome", Tag: "kosync",
			Status: http.StatusForbidden, Permission: auth.Public, Handler: s.kosyncRegister,
		},
		{
			Method: http.MethodPut, Path: "/kosync/syncs/progress", ID: "kosyncPush",
			Summary: "KOReader reports where the person is in a document, named by its partial MD5", Tag: "kosync",
			Request: kosyncProgress{}, Response: kosyncSaved{}, Permission: auth.PersonalManage, Devices: true, Handler: s.kosyncPush,
		},
		{
			Method: http.MethodGet, Path: "/kosync/syncs/progress/{document}", ID: "kosyncPull",
			Summary: "Where the person is in a document, as KOReader goes to it; empty where no place can be given", Tag: "kosync",
			Response: kosyncPosition{}, Reads: ReadsLibraries, Permission: auth.PersonalManage, Devices: true, Handler: s.kosyncPull,
		},
	}
}

func (s *Server) kosyncAuth(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, http.StatusOK, kosyncAuthorized{Authorized: "OK"})
	return nil
}

func (s *Server) kosyncRegister(w http.ResponseWriter, r *http.Request) error {
	return ErrForbidden("Accounts are made in GOtome; sign in with your user name and a device token from your profile.")
}

func (s *Server) kosyncPush(w http.ResponseWriter, r *http.Request) error {
	var req kosyncProgress
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if req.Document == "" || req.Progress == "" {
		return ErrValidation(map[string]string{"document": "Name the document and the place in it."})
	}
	at, err := s.Sync.Push(r.Context(), library.ScopeOf(*UserFrom(r.Context())), kosync.Position{
		Document: req.Document, Progress: req.Progress, Percentage: req.Percentage, Device: req.Device, DeviceID: req.DeviceID,
	})
	var problems reading.Problems
	switch {
	case errors.Is(err, kosync.ErrUnknown):
		return ErrNotFound("GOtome has no book with this file, or not one you may see.")
	case errors.As(err, &problems):
		return ErrValidation(problems)
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusOK, kosyncSaved{Document: req.Document, Timestamp: at.Unix()})
	return nil
}

func (s *Server) kosyncPull(w http.ResponseWriter, r *http.Request) error {
	document := chi.URLParam(r, "document")
	p, ok, err := s.Sync.Pull(r.Context(), library.ScopeOf(*UserFrom(r.Context())), document)
	if err != nil && !errors.Is(err, kosync.ErrUnknown) {
		return err
	}
	// KOReader takes an empty answer as no place saved, which is also what
	// a document of no book the person sees gets.
	if !ok {
		writeJSON(w, r, http.StatusOK, kosyncPosition{})
		return nil
	}
	writeJSON(w, r, http.StatusOK, kosyncPosition{
		Document: p.Document, Progress: p.Progress, Percentage: p.Percentage,
		Device: p.Device, DeviceID: p.DeviceID, Timestamp: p.Timestamp.Unix(),
	})
	return nil
}
