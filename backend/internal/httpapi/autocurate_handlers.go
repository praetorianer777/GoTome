package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/autocurate"
	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/cleanup"
	"github.com/praetorianer777/gotome/backend/internal/enrich"
	"github.com/praetorianer777/gotome/backend/internal/library"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

type autoCleanupKind struct {
	Kind string `json:"kind" enum:"placeholders,titles,authors,series"`
	// Suggestions is how many suggestions of the kind a run would apply,
	// and Books how many books they change.
	Suggestions int `json:"suggestions"`
	Books       int `json:"books"`
}

type autoCleanupCounts struct {
	Kinds []autoCleanupKind `json:"kinds"`
}

type autoRunRequest struct {
	// Kinds are the kinds of clean-up to apply; at least one.
	Kinds []string `json:"kinds,omitempty"`
	// DryRun records what the rules would do, with their reasons, and
	// changes nothing.
	DryRun bool `json:"dryRun"`
	// Provider and Band, together, limit a run over matches to one group a
	// person accepted as a whole.
	Provider string `json:"provider,omitempty"`
	Band     *int   `json:"band,omitempty" doc:"The tenth of the score, 0 to 9; 9 is 0.9 and over."`
}

type autoRunStarted struct {
	// RunID is the run on the Changes page, GET /curation/runs/{id}.
	RunID  uuid.UUID `json:"runId"`
	DryRun bool      `json:"dryRun"`
}

type autoMatchGroup struct {
	Provider string `json:"provider"`
	Band     int    `json:"band"`
	Matches  int    `json:"matches"`
	// Sample is some of its matches, the best fitting first, to look at
	// before accepting the group.
	Sample []reviewBook `json:"sample"`
}

type autoMatchCounts struct {
	// Accept is how many matches a run would take fields from, Settle how
	// many of them it takes whole and Dismiss how many it would dismiss,
	// among the first Books books.
	Accept  int `json:"accept"`
	Settle  int `json:"settle"`
	Dismiss int `json:"dismiss"`
	Books   int `json:"books"`
	// Capped says there are more books than were looked at.
	Capped bool             `json:"capped"`
	Groups []autoMatchGroup `json:"groups"`
}

func (s *Server) autoCleanupCounts(w http.ResponseWriter, r *http.Request) error {
	counts, err := s.Auto.CleanupCounts(r.Context(), library.ScopeOf(*UserFrom(r.Context())))
	if err != nil {
		return err
	}
	out := autoCleanupCounts{Kinds: make([]autoCleanupKind, len(counts))}
	for i, c := range counts {
		out.Kinds[i] = autoCleanupKind{Kind: c.Kind, Suggestions: c.Suggestions, Books: c.Books}
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) startAutoCleanup(w http.ResponseWriter, r *http.Request) error {
	var req autoRunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	user := UserFrom(r.Context())
	started, err := s.Auto.StartCleanup(r.Context(), library.ScopeOf(*user), &user.ID, req.Kinds, req.DryRun)
	if errors.Is(err, autocurate.ErrKinds) {
		return ErrValidation(map[string]string{"kinds": "Name at least one of " + joined(cleanup.AutoKinds) + "."})
	}
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, autoRunStarted{RunID: started.RunID, DryRun: started.DryRun})
	return nil
}

func joined(list []string) string {
	out := ""
	for i, s := range list {
		switch {
		case i == 0:
		case i == len(list)-1:
			out += " or "
		default:
			out += ", "
		}
		out += s
	}
	return out
}

func (s *Server) autoMatchCounts(w http.ResponseWriter, r *http.Request) error {
	user := UserFrom(r.Context())
	scope := library.ScopeOf(*user)
	plan, err := s.Auto.MatchPlan(r.Context(), scope)
	if err != nil {
		return err
	}
	groups, err := s.Auto.Groups(r.Context(), scope)
	if err != nil {
		return err
	}
	out := autoMatchCounts{
		Accept: plan.Accept, Settle: plan.Settled, Dismiss: plan.Dismiss, Books: plan.Books, Capped: plan.Capped,
		Groups: make([]autoMatchGroup, 0, len(groups)),
	}
	for _, g := range groups {
		item := autoMatchGroup{Provider: g.Provider, Band: g.Band, Matches: g.Matches, Sample: []reviewBook{}}
		for _, id := range g.Sample {
			m, err := s.Matches.Pending(r.Context(), scope, id)
			if errors.Is(err, enrich.ErrNoMatch) {
				continue
			}
			if err != nil {
				return err
			}
			b, err := s.Books.Get(r.Context(), scope, m.BookID)
			if errors.Is(err, catalog.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			item.Sample = append(item.Sample, reviewBook{
				Book: detailOf(b, user),
				Matches: []matchView{{
					MatchID:       m.ID,
					candidateView: s.candidateViewOf(b, metadata.Candidate{Record: m.Record, Score: m.Score}),
				}},
			})
		}
		out.Groups = append(out.Groups, item)
	}
	writeJSON(w, r, http.StatusOK, out)
	return nil
}

func (s *Server) startAutoMatches(w http.ResponseWriter, r *http.Request) error {
	var req autoRunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		return err
	}
	var group *autocurate.MatchGroup
	if req.Provider != "" || req.Band != nil {
		if req.Provider == "" || req.Band == nil || *req.Band < 0 || *req.Band > 9 {
			return ErrValidation(map[string]string{"band": "Name a provider and a band from 0 to 9 together."})
		}
		group = &autocurate.MatchGroup{Provider: req.Provider, Band: *req.Band}
	}
	user := UserFrom(r.Context())
	started, err := s.Auto.StartMatches(r.Context(), library.ScopeOf(*user), &user.ID, group, req.DryRun)
	if err != nil {
		return err
	}
	writeJSON(w, r, http.StatusAccepted, autoRunStarted{RunID: started.RunID, DryRun: started.DryRun})
	return nil
}
