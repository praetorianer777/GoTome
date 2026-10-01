package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type listJobsQuery struct {
	State string `query:"state" enum:"waiting,running,failed,done" doc:"Only jobs in this state; left out, all."`
	Limit int    `query:"limit" doc:"How many of the newest jobs, at most 200; 100 when left out."`
}

// jobView is a background job and what it works on.
type jobView struct {
	ID int64 `json:"id"`
	// Kind names what the job does, such as ingest.scan_library.
	Kind string `json:"kind"`
	// State is the queue's own: available, scheduled, running, retryable,
	// completed, cancelled or discarded.
	State       string     `json:"state"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"maxAttempts"`
	CreatedAt   time.Time  `json:"createdAt"`
	ScheduledAt time.Time  `json:"scheduledAt"`
	AttemptedAt *time.Time `json:"attemptedAt,omitempty"`
	FinalizedAt *time.Time `json:"finalizedAt,omitempty"`
	// LastError is what the latest failed attempt said.
	LastError   string     `json:"lastError,omitempty"`
	LibraryID   *uuid.UUID `json:"libraryId,omitempty"`
	LibraryName string     `json:"libraryName,omitempty"`
	// FileID, FilePath and BookID are the file a reading job reads.
	FileID   *uuid.UUID `json:"fileId,omitempty"`
	FilePath string     `json:"filePath,omitempty"`
	BookID   *uuid.UUID `json:"bookId,omitempty"`
}

type jobList struct {
	Jobs []jobView `json:"jobs"`
}

type rereadLibraryRequest struct {
	// FailedOnly reads only the files that could not be read before;
	// otherwise every file is read again.
	FailedOnly bool `json:"failedOnly"`
}

// rereadResult is how many files were queued to be read again.
type rereadResult struct {
	Queued int `json:"queued"`
}

const defaultJobs = 100

func toJobView(j ingest.JobStatus) jobView {
	return jobView{
		ID: j.ID, Kind: j.Kind, State: j.State, Attempt: j.Attempt, MaxAttempts: j.MaxAttempts,
		CreatedAt: j.CreatedAt, ScheduledAt: j.ScheduledAt, AttemptedAt: j.AttemptedAt, FinalizedAt: j.FinalizedAt,
		LastError: j.LastError, LibraryID: j.LibraryID, LibraryName: j.LibraryName,
		FileID: j.FileID, FilePath: j.FilePath, BookID: j.BookID,
	}
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) error {
	var q listJobsQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	var groups []string
	if q.State != "" {
		groups = []string{q.State}
	}
	found, err := s.Scans.Jobs(r.Context(), library.ScopeOf(*UserFrom(r.Context())), groups, cmp.Or(q.Limit, defaultJobs))
	if err != nil {
		return err
	}
	out := jobList{Jobs: make([]jobView, len(found))}
	for i, j := range found {
		out.Jobs[i] = toJobView(j)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) retryJob(w http.ResponseWriter, r *http.Request) error {
	return s.actOnJob(w, r, s.Scans.RetryJob)
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) error {
	return s.actOnJob(w, r, s.Scans.CancelJob)
}

func (s *Server) actOnJob(w http.ResponseWriter, r *http.Request, act func(context.Context, library.Scope, int64) (ingest.JobStatus, error)) error {
	id, err := jobID(r)
	if err != nil {
		return err
	}
	j, err := act(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, ingest.ErrNoJob) {
		return ErrNotFound("There is no such job.")
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, toJobView(j))
	return nil
}

func jobID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "jobId"), 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrNotFound("There is no such job.")
	}
	return id, nil
}

func (s *Server) rereadFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	if _, err := s.Books.VisibleFileID(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); errors.Is(err, catalog.ErrNotFound) {
		return ErrNotFound("There is no such file.")
	} else if err != nil {
		return err
	}
	n, err := s.Scans.Reread(r.Context(), []uuid.UUID{id})
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, rereadResult{Queued: n})
	return nil
}

func (s *Server) rereadLibrary(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "libraryId", "library")
	if err != nil {
		return err
	}
	var req rereadLibraryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if _, err := s.Libraries.Get(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id); errors.Is(err, library.ErrNotFound) {
		return ErrNotFound("There is no such library.")
	} else if err != nil {
		return err
	}
	files, err := s.Scans.LibraryFiles(r.Context(), id, req.FailedOnly)
	if err != nil {
		return err
	}
	n, err := s.Scans.Reread(r.Context(), files)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, rereadResult{Queued: n})
	return nil
}
