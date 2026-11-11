package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// scoringRun asks for a measure or a learning of some books; none is the
// default number.
type scoringRun struct {
	Books int `json:"books,omitempty" doc:"How many books with an ISBN to look up at most; 0 is 200 for a measure and 1000 for a learning."`
}

func (s *Server) getScoring(w http.ResponseWriter, r *http.Request) error {
	status, err := s.Metadata.Scoring(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, status)
	return nil
}

func (s *Server) requestScoringRun(learn bool) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req scoringRun
		if err := decodeJSON(w, r, &req); err != nil {
			return err
		}
		if req.Books < 0 || req.Books > metadata.MaxRunBooks {
			return ErrValidation(map[string]string{"books": "Ask for at most 5000 books."})
		}
		var err error
		if learn {
			err = s.Metadata.RequestLearn(r.Context(), req.Books)
		} else {
			err = s.Metadata.RequestMeasure(r.Context(), req.Books)
		}
		if errors.Is(err, metadata.ErrRunBusy) {
			return ErrConflict("This is already running.")
		}
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusAccepted)
		return nil
	}
}

func (s *Server) exportScoring(w http.ResponseWriter, r *http.Request) error {
	p, err := s.Metadata.ExportProfile(r.Context())
	if errors.Is(err, metadata.ErrNoLearnedWeights) {
		return ErrNotFound("Nothing has been learned yet.")
	}
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Disposition", `attachment; filename="gotome-match-weights.json"`)
	h.Set("Cache-Control", "no-store")
	_, err = w.Write(append(encoded, '\n'))
	return err
}

func (s *Server) importScoring(w http.ResponseWriter, r *http.Request) error {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<10))
	if err != nil {
		return ErrBadRequest("The file is too large or cannot be read.")
	}
	err = s.Metadata.ImportProfile(r.Context(), raw)
	if errors.Is(err, metadata.ErrProfileVersion) || errors.Is(err, metadata.ErrProfileInvalid) {
		return ErrValidation(map[string]string{"file": err.Error() + "."})
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) deleteImportedScoring(w http.ResponseWriter, r *http.Request) error {
	if err := s.Metadata.RemoveImported(r.Context()); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
