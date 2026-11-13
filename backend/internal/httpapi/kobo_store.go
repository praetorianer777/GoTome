package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"

	"github.com/praetorianer777/gotome/backend/internal/auth"
)

// decodeKobo reads what a Kobo reader sends. Its firmware adds fields from
// one version to the next, so unknown ones are skipped, unlike decodeJSON.
func decodeKobo(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return ErrBadRequest("The request body is too large.")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return ErrBadRequest("The request body is not valid JSON.")
	}
	return nil
}

// koboItems is the shape of a store list that holds nothing.
type koboItems struct {
	Items          []any `json:"Items"`
	TotalPageCount int   `json:"TotalPageCount"`
	PageIndex      int   `json:"PageIndex"`
	PageSize       int   `json:"PageSize"`
}

// koboNoItems answers the store pages a reader builds from its endpoint
// (featured lists, what to read next) with a list of nothing, so it shows
// no error where the store has nothing to say.
func (s *Server) koboNoItems(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, r, http.StatusOK, koboItems{Items: []any{}})
	return nil
}

// koboAnalyticsEvents is what a reader reports for the store's analytics.
// GOtome keeps none of it; it logs which kinds of event and which names of
// attributes and metrics arrive, never a value, so that an owner can see
// what a firmware sends.
type koboAnalyticsEvents struct {
	Events []struct {
		EventType  string         `json:"EventType"`
		Attributes map[string]any `json:"Attributes"`
		Metrics    map[string]any `json:"Metrics"`
	} `json:"Events"`
}

func (s *Server) koboAnalytics(w http.ResponseWriter, r *http.Request) error {
	var in koboAnalyticsEvents
	if err := decodeKobo(w, r, &in); err == nil && s.Log != nil && s.Log.Enabled(r.Context(), slog.LevelDebug) {
		keys := func(m map[string]any) []string {
			out := make([]string, 0, len(m))
			for k := range m {
				out = append(out, k)
			}
			sort.Strings(out)
			return out
		}
		for _, e := range in.Events {
			s.Log.Debug("kobo analytics event", "type", e.EventType, "attributes", keys(e.Attributes), "metrics", keys(e.Metrics))
		}
	}
	return s.koboNothing(w, r)
}

// koboStoreRoutes are the store's pages a reader asks GOtome for because it
// builds their addresses from its endpoint.
func (s *Server) koboStoreRoutes() []Route {
	empty := func(path, id, summary string) Route {
		return Route{
			Method: http.MethodGet, Path: "/kobo/{token}" + path, ID: id, Summary: summary, Tag: "kobo",
			Reads: ReadsNoLibrary, Permission: auth.LibraryRead, Devices: true, Handler: s.koboNoItems,
		}
	}
	return []Route{
		empty("/v1/products/featured", "koboFeatured", "The store's featured lists: none"),
		empty("/v1/products/featured/", "koboFeaturedSlash", "The same, as the reader writes the address"),
		empty("/v1/products/featured/{listId}", "koboFeaturedList", "One of the store's featured lists: empty"),
		empty("/v1/products/{productId}/nextread", "koboNextRead", "What the store suggests reading next: nothing"),
		{
			Method: http.MethodGet, Path: "/kobo/{token}/v1/affiliate", ID: "koboAffiliate",
			Summary: "The affiliate a Kobo reader asks the store for: none", Tag: "kobo",
			Reads: ReadsNoLibrary, Permission: auth.LibraryRead, Devices: true, Handler: s.koboNothing,
		},
	}
}
