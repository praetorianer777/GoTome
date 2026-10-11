package httpapi

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/koplugin"
)

type deviceView struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type deviceList struct {
	Devices []deviceView `json:"devices"`
}

type createDeviceRequest struct {
	Name string `json:"name"`
}

type createdDevice struct {
	Device deviceView `json:"device"`
	// Token is what the device signs in with, as the password beside the
	// user name. It is never shown again.
	Token string `json:"token"`
	// Catalog is the address of the catalogue for e-readers.
	Catalog string `json:"catalog"`
}

func viewOf(d auth.Device) deviceView {
	return deviceView{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, LastUsedAt: d.LastUsedAt}
}

func (s *Server) listOwnDevices(w http.ResponseWriter, r *http.Request) error {
	devices, err := s.Auth.Devices(r.Context(), UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	out := deviceList{Devices: make([]deviceView, len(devices))}
	for i, d := range devices {
		out.Devices[i] = viewOf(d)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) createOwnDevice(w http.ResponseWriter, r *http.Request) error {
	var req createDeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	device, token, err := s.Auth.CreateDevice(r.Context(), UserFrom(r.Context()).ID, req.Name)
	var invalid *auth.ValidationError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid.Fields)
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusCreated, createdDevice{Device: viewOf(device), Token: token, Catalog: opdsPath})
	return nil
}

func (s *Server) deleteOwnDevice(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "deviceId", "device")
	if err != nil {
		return err
	}
	err = s.Auth.DeleteDevice(r.Context(), UserFrom(r.Context()).ID, id)
	if errors.Is(err, auth.ErrNoDevice) {
		return ErrNotFound("There is no such device.")
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// getKOReaderPlugin sends the KOReader plugin as a zip, told the address
// the browser used as its server.
func (s *Server) getKOReaderPlugin(w http.ResponseWriter, r *http.Request) error {
	scheme := "http"
	if overTLS(r) {
		scheme = "https"
	}
	var buf bytes.Buffer
	if err := koplugin.Zip(&buf, scheme+"://"+r.Host); err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="gotome.koplugin.zip"`)
	h.Set("Cache-Control", "no-cache")
	_, err := buf.WriteTo(w)
	return err
}
