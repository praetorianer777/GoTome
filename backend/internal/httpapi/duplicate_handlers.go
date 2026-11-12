package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/autodupe"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/ingest"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

type duplicatesQuery struct {
	State     string `query:"state" enum:"open,kept_both,merged,replaced" doc:"Which pairs: open (the default) are those nobody has decided about."`
	Library   string `query:"library" doc:"A library's ID: the pairs with a book in it. Left out, every library the caller may see."`
	Kind      string `query:"kind" enum:"sha256,content,isbn,title_author,overlap" doc:"Only pairs with evidence of this kind."`
	Contained string `query:"contained" enum:"true,false" doc:"true: only pairs of which one book holds the other's text, such as a story and its collection; false: only the others. Left out, both."`
	Least     int    `query:"least" doc:"Only pairs whose score is at least this many percent."`
	Cursor    string `query:"cursor" doc:"Where the page before ended, as its nextCursor says; pairs come strongest first."`
	Limit     int    `query:"limit" doc:"How many pairs a page holds, at most 100; 50 when left out."`
}

type duplicateEvidence struct {
	Kind string `json:"kind"`
	// Detail is what the books share: the hash, the ISBN, the title and
	// author as compared, or for overlap "jaccard=… a_in_b=… b_in_a=…",
	// from the pair's first book.
	Detail string `json:"detail"`
	// Score is how sure this evidence alone makes it, from 0 to 1.
	Score float32 `json:"score"`
}

// duplicatePair is two books that look like one, both visible to the
// caller, and why.
type duplicatePair struct {
	ID    uuid.UUID `json:"id"`
	State string    `json:"state"`
	// Score is how sure the strongest evidence makes it that the books are
	// one, from 0 to 1.
	Score float32 `json:"score"`
	// Contained says that one book holds the other's text, such as a
	// story and its collection, without the two being one text: all its
	// evidence is shared text of an overlap under 50%.
	Contained bool                `json:"contained"`
	FoundAt   time.Time           `json:"foundAt"`
	Books     []bookSummary       `json:"books"`
	Evidence  []duplicateEvidence `json:"evidence"`
	// Narrators, when both books are audiobooks read by different people,
	// lists the narrators of each book in the order of books: the pair is
	// one book recorded twice, to be linked as recordings rather than
	// merged. Left out otherwise.
	Narrators [][]string `json:"narrators,omitempty"`
	// Formats is set for an ebook and its audiobook: the pair's books with
	// the same title and author, one of only ebook files, the other of
	// only audio files. Left out otherwise.
	Formats *pairFormats `json:"formats,omitempty"`
}

type pairFormats struct {
	// EbookID is the pair's book that holds the ebook.
	EbookID uuid.UUID `json:"ebookId"`
	// SameLibrary says a merge can join them; books of two libraries are
	// only linked (relation format), as a file stays in its library's folder.
	SameLibrary bool `json:"sameLibrary"`
}

type duplicateList struct {
	Pairs []duplicatePair `json:"pairs"`
	// NextCursor asks for the pairs that follow; left out when none do.
	NextCursor string `json:"nextCursor,omitempty"`
}

type pairStateRequest struct {
	// State is kept_both, that both books stay as they are, or open.
	State string `json:"state"`
	// Relation, with kept_both, links the two as another edition, a
	// translation, another recording or a related book.
	Relation string `json:"relation,omitempty"`
}

type replaceRequest struct {
	// Keep is the pair's book that stays; the other's files go to the
	// trash and the book into this one.
	Keep uuid.UUID `json:"keep"`
}

type duplicateCheckRequest struct {
	// Library names the library whose books are checked; left out, every
	// library the caller sees.
	Library *uuid.UUID `json:"library,omitempty"`
}

type duplicateCheckResult struct {
	// Books is how many books were queued to be checked.
	Books int `json:"books"`
}

const defaultDuplicatePage = 50

func (s *Server) listDuplicates(w http.ResponseWriter, r *http.Request) error {
	var q duplicatesQuery
	if err := decodeQuery(r, &q); err != nil {
		return err
	}
	lq := dedupe.ListQuery{State: dedupe.StateOpen, Limit: defaultDuplicatePage}
	if q.State != "" {
		lq.State = q.State
	}
	if q.Limit > 0 {
		lq.Limit = q.Limit
	}
	if q.Library != "" {
		id, err := uuid.Parse(q.Library)
		if err != nil {
			return ErrValidation(map[string]string{"library": "There is no such library."})
		}
		lq.LibraryID = &id
	}
	if q.Kind != "" {
		lq.Kind = &q.Kind
	}
	if q.Contained != "" {
		contained := q.Contained == "true"
		lq.Contained = &contained
	}
	lq.Least = float32(q.Least) / 100
	if q.Cursor != "" {
		score, id, _ := strings.Cut(q.Cursor, "_")
		f, scoreErr := strconv.ParseFloat(score, 32)
		pairID, idErr := uuid.Parse(id)
		if scoreErr != nil || idErr != nil {
			return ErrValidation(map[string]string{"cursor": "Give the nextCursor of the page before."})
		}
		lq.After = &dedupe.Cursor{Score: float32(f), ID: pairID}
	}
	pairs, more, err := s.Duplicates.List(r.Context(), library.ScopeOf(*UserFrom(r.Context())), lq)
	if err != nil {
		return err
	}
	out := duplicateList{Pairs: make([]duplicatePair, len(pairs))}
	if more && len(pairs) > 0 {
		last := pairs[len(pairs)-1]
		out.NextCursor = strconv.FormatFloat(float64(last.Score), 'g', -1, 32) + "_" + last.ID.String()
	}
	for i, p := range pairs {
		dp := duplicatePair{ID: p.ID, State: p.State, Score: p.Score, Contained: p.Contained, FoundAt: p.FoundAt, Evidence: []duplicateEvidence{}, Narrators: p.Narrators}
		if p.Formats != nil {
			dp.Formats = &pairFormats{EbookID: p.Formats.EbookBook, SameLibrary: p.Formats.SameLibrary}
		}
		for _, b := range p.Books {
			dp.Books = append(dp.Books, summaryOf(b))
		}
		for _, e := range p.Evidence {
			dp.Evidence = append(dp.Evidence, duplicateEvidence{Kind: e.Kind, Detail: e.Detail, Score: e.Score})
		}
		out.Pairs[i] = dp
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) setPairState(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "pairId", "pair")
	if err != nil {
		return err
	}
	var req pairStateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	err = s.Duplicates.SetState(r.Context(), library.ScopeOf(*UserFrom(r.Context())), id, req.State, req.Relation)
	switch {
	case errors.Is(err, dedupe.ErrNotFound):
		return ErrNotFound("There is no such pair.")
	case errors.Is(err, dedupe.ErrState):
		return ErrValidation(map[string]string{"state": "Use kept_both or open."})
	case errors.Is(err, dedupe.ErrRelation):
		return ErrValidation(map[string]string{"relation": "With kept_both, use edition, translation, recording, format or related."})
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) replaceDuplicate(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "pairId", "pair")
	if err != nil {
		return err
	}
	var req replaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	a, b, state, err := s.Duplicates.Books(r.Context(), scope, id)
	if errors.Is(err, dedupe.ErrNotFound) {
		return ErrNotFound("There is no such pair.")
	}
	if err != nil {
		return err
	}
	if state != dedupe.StateOpen {
		return ErrConflict("This pair has been decided about; open it again first.")
	}
	gone := a
	switch req.Keep {
	case a:
		gone = b
	case b:
	default:
		return ErrValidation(map[string]string{"keep": "Name one of the pair's two books."})
	}
	err = s.Scans.Replace(r.Context(), scope, req.Keep, gone)
	switch {
	case errors.Is(err, catalog.ErrNotFound):
		return ErrNotFound("There is no such pair.")
	case errors.Is(err, ingest.ErrMergeLibraries):
		return ErrValidation(map[string]string{"keep": "Only books of one library are replaced: a file stays in its library's folder."})
	case errors.Is(err, ingest.ErrReadOnly):
		return ErrConflict("This library is read-only: GOtome does not move its files.")
	case errors.Is(err, ingest.ErrFolderReadOnly):
		return folderReadOnly(err)
	case err != nil:
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) checkDuplicates(w http.ResponseWriter, r *http.Request) error {
	var req duplicateCheckRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	scope := library.ScopeOf(*UserFrom(r.Context()))
	if req.Library != nil {
		if _, err := s.Libraries.Get(r.Context(), scope, *req.Library); err != nil {
			return libraryError(err)
		}
	}
	n, err := s.Duplicates.Request(r.Context(), scope, req.Library)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, duplicateCheckResult{Books: n})
	return nil
}

type duplicateClassCount struct {
	// Class is one of same_text, overlap_high, overlap_mid, contained and
	// part_inside.
	Class string `json:"class" enum:"same_text,overlap_high,overlap_mid,contained,part_inside"`
	// Pairs is how many open pairs of two books of one library the caller sees
	// fall in it.
	Pairs int `json:"pairs"`
	// Merges says whether the pairs are made one book; else they are kept and
	// linked.
	Merges bool `json:"merges"`
	// Daily says that the settings have the class resolved every day.
	Daily bool `json:"daily"`
}

type duplicateClassList struct {
	Classes []duplicateClassCount `json:"classes"`
}

type duplicateRunRequest struct {
	Classes []string `json:"classes" doc:"The classes to resolve, from the list of classes."`
	DryRun  bool     `json:"dryRun" doc:"Record what would be done, with the reasons, and change nothing."`
}

type duplicateRunStarted struct {
	// RunID is the run that records it, to follow on the changes page.
	RunID uuid.UUID `json:"runId"`
	// DryRun says that the run only records what it would do.
	DryRun bool `json:"dryRun"`
	// Pairs is how many open pairs of the classes it starts with.
	Pairs int `json:"pairs"`
}

func (s *Server) listDuplicateClasses(w http.ResponseWriter, r *http.Request) error {
	scope := library.ScopeOf(*UserFrom(r.Context()))
	counts, err := s.AutoDuplicates.Counts(r.Context(), scope)
	if err != nil {
		return err
	}
	daily, err := s.AutoDuplicates.Classes(r.Context())
	if err != nil {
		return err
	}
	out := duplicateClassList{Classes: make([]duplicateClassCount, len(dedupe.AutoClasses))}
	for i, c := range dedupe.AutoClasses {
		out.Classes[i] = duplicateClassCount{Class: c, Pairs: counts[c], Merges: dedupe.Merges(c), Daily: slices.Contains(daily, c)}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) startDuplicateRun(w http.ResponseWriter, r *http.Request) error {
	var req duplicateRunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	started, err := s.AutoDuplicates.Start(r.Context(), library.ScopeOf(*UserFrom(r.Context())), req.Classes, req.DryRun)
	switch {
	case errors.Is(err, autodupe.ErrClasses):
		return ErrValidation(map[string]string{"classes": "Choose at least one of same_text, overlap_high, overlap_mid, contained and part_inside."})
	case errors.Is(err, autodupe.ErrBusy):
		return ErrConflict("A run of the duplicate rules is going.")
	case err != nil:
		return err
	}
	writeJSON(w, r, http.StatusAccepted, duplicateRunStarted{RunID: started.RunID, DryRun: req.DryRun, Pairs: started.Pairs})
	return nil
}
