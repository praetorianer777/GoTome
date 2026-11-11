package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/format/epub"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type trashQuery struct {
	Library string `query:"library" doc:"A library's ID; left out, every library the caller may see."`
}

// trashedFile is a file in the trash, until it is restored or purged.
type trashedFile struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Format    string    `json:"format"`
	Size      int64     `json:"size"`
	BookID    uuid.UUID `json:"bookId"`
	BookTitle string    `json:"bookTitle"`
	LibraryID uuid.UUID `json:"libraryId"`
	Library   string    `json:"library"`
	TrashedAt time.Time `json:"trashedAt"`
	// TrashedBy is left out when the account is gone.
	TrashedBy string `json:"trashedBy,omitempty"`
	// PurgeAt is when it is deleted for good, by the retention set now.
	PurgeAt time.Time `json:"purgeAt"`
}

type trashList struct {
	Files []trashedFile `json:"files"`
}

// trashError maps what trashing, restoring and purging refuse.
func trashError(err error) error {
	switch {
	case errors.Is(err, ingest.ErrNoFile):
		return ErrNotFound("There is no such file in the trash.")
	case errors.Is(err, ingest.ErrReadOnly):
		return ErrConflict("This library is read-only: GOtome does not move its files.")
	case errors.Is(err, ingest.ErrFolderReadOnly):
		return folderReadOnly(err)
	case errors.Is(err, ingest.ErrFileMissing):
		return ErrConflict("This file is missing from its folder.")
	case errors.Is(err, ingest.ErrPathTaken):
		return ErrConflict("Another file is where this one was. Move it away first.")
	}
	return err
}

// folderReadOnly is a writable library whose folder, or a folder in it,
// cannot be written to now. Its reason is the system's, such as
// "read-only file system".
func folderReadOnly(err error) error {
	where := "this library's folder"
	if folder := ingest.FolderOf(err); folder != "" {
		where = folder
	}
	return ErrConflict("GOtome cannot write to " + where + " now (" + ingest.FolderReason(err) +
		"). If its mount was made read-only, mount it writable again; otherwise the app's user 10001 needs " +
		"write access to it. The Libraries page says more.")
}

func (s *Server) trashFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	err = s.Scans.Trash(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)
	if errors.Is(err, ingest.ErrNoFile) {
		return ErrNotFound("There is no such file.")
	}
	if err := trashError(err); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) restoreFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	if err := trashError(s.Scans.Restore(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) purgeFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	if err := trashError(s.Scans.Purge(r.Context(), id)); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) listTrash(w http.ResponseWriter, r *http.Request) error {
	var q trashQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	var libraryID *uuid.UUID
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		}
		libraryID = &id
	}
	files, err := s.Scans.ListTrash(r.Context(), library.ScopeOf(*UserFrom(r.Context())), libraryID)
	if err != nil {
		return err
	}
	retention, err := s.Settings.TrashRetention(r.Context())
	if err != nil {
		return err
	}
	out := trashList{Files: make([]trashedFile, len(files))}
	for i, f := range files {
		out.Files[i] = trashedFile{
			ID: f.ID, Name: path.Base(f.RelPath), Format: f.Format, Size: f.SizeBytes,
			BookID: f.BookID, BookTitle: f.Title, LibraryID: f.LibraryID, Library: f.LibraryName,
			TrashedAt: f.TrashedAt, PurgeAt: f.TrashedAt.Add(retention),
		}
		if f.TrashedBy != nil {
			out.Files[i].TrashedBy = *f.TrashedBy
		}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

// fixRequest names what to fix in a file, of the findings its ereader
// check names; subset-fonts fixes large-fonts by keeping the fonts, cut down
// to the characters the book uses, instead of removing them.
type fixRequest struct {
	Findings []string `json:"findings"`
}

// fixResult says what a fix changed. The original is in the trash, from
// which it can be restored beside the fixed file.
type fixResult struct {
	DocumentsSplit int `json:"documentsSplit"`
	PartsAdded     int `json:"partsAdded"`
	ImagesScaled   int `json:"imagesScaled"`
	FontsSubset    int `json:"fontsSubset"`
	FontsRemoved   int `json:"fontsRemoved"`
	ItemsRemoved   int `json:"itemsRemoved"`
	// DocumentsRepaired are documents written again as well-formed XHTML.
	DocumentsRepaired int `json:"documentsRepaired"`
	// TocEntriesRemoved are entries of the tables of contents that led
	// nowhere and were taken out, TocEntriesRepointed those pointed at
	// the start of their document or at an entry below them.
	TocEntriesRemoved   int `json:"tocEntriesRemoved"`
	TocEntriesRepointed int `json:"tocEntriesRepointed"`
}

func (s *Server) fixFile(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "fileId", "file")
	if err != nil {
		return err
	}
	var req fixRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Findings) == 0 {
		return ErrValidation(map[string]string{"findings": "Say what to fix."})
	}
	for _, f := range req.Findings {
		if !slices.Contains(epub.Fixes, f) {
			return ErrValidation(map[string]string{"findings": fmt.Sprintf("%q is not a fix; one is %s.", f, strings.Join(epub.Fixes, ", "))})
		}
	}
	fixed, err := s.Scans.Fix(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.Findings)
	switch {
	case errors.Is(err, ingest.ErrNoFile):
		return ErrNotFound("There is no such file.")
	case errors.Is(err, ingest.ErrNotFixable):
		return ErrConflict("GOtome cannot change this file: it is no EPUB it can read, or it is protected by DRM.")
	case errors.Is(err, ingest.ErrNothingToFix):
		return ErrConflict("There was nothing to fix in this file.")
	case errors.Is(err, ingest.ErrFileChanged):
		return ErrConflict("The file changed since it was read. It is read again at the next scan.")
	}
	if err := trashError(err); err != nil {
		return err
	}
	writeJSON(w, r, http.StatusOK, fixResult(fixed))
	return nil
}
