package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

// settingList is every setting, in the order a page shows them.
type settingList struct {
	Settings []settingView `json:"settings"`
}

type settingView struct {
	Key string `json:"key"`
	// Kind is text or secret. A secret is never sent back: only isSet says
	// that there is one.
	Kind string `json:"kind"`
	// Value is a text setting's value, or its default while it is unset.
	Value     string     `json:"value,omitempty"`
	IsSet     bool       `json:"isSet"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

type updateSettingsRequest struct {
	// Values maps a setting's key to its new value; null or "" unsets it.
	// Settings left out stay as they are.
	Values map[string]*string `json:"values"`
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) error {
	return s.writeSettings(w, r)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) error {
	var req updateSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err := s.Settings.Update(r.Context(), UserFrom(r.Context()).ID, req.Values)
	var invalid *settings.ValidationError
	if errors.As(err, &invalid) {
		return ErrValidation(invalid.Fields)
	}
	if err != nil {
		return err
	}
	return s.writeSettings(w, r)
}

func (s *Server) writeSettings(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Settings.List(r.Context())
	if err != nil {
		return err
	}
	out := settingList{Settings: make([]settingView, len(list))}
	for i, st := range list {
		out.Settings[i] = settingView{Key: st.Key, Kind: st.Kind, Value: st.Value, IsSet: st.IsSet, UpdatedAt: st.UpdatedAt}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}
