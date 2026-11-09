package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/praetorianer777/gotome/backend/internal/auth"
	"github.com/praetorianer777/gotome/backend/internal/httpapi"
)

// Options are how the views are timed.
type Options struct {
	// Skip names views not to run.
	Skip []string
	// Tries is how many times a view is timed after its warm-up; the best
	// counts. Timeout bounds each request, Pause is left between views.
	Tries   int
	Timeout time.Duration
	Pause   time.Duration
	// Started is told the name of each view as it begins.
	Started func(name string)
}

// Why a view was not timed.
const (
	NotSkipped = ""
	Skipped    = "skipped"
	NoData     = "nothing in the library to ask about"
	NotAllowed = "not for this person"
)

// Result is one view's timing.
type Result struct {
	Name string
	// Not is why the view was not timed, if it was not.
	Not string
	// Best is the shortest of the timed requests.
	Best time.Duration
	// Rows is the entries of the longest list in the answer, -1 for an
	// answer with none.
	Rows int
	// Whole says why the view reads every book, if it does.
	Whole string
}

// Measure asks each view of the handler as the user, one at a time: once to
// warm up, then Tries times, keeping the best. It stops at the first answer
// that is not a success. An error names the view and the status, never the
// address or the answer, which can hold names from the library.
func Measure(ctx context.Context, handler http.Handler, user auth.User, views []View, o Options) ([]Result, error) {
	if o.Tries < 1 {
		o.Tries = 3
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	admin := auth.Allows(user.Role, auth.UsersManage)
	var results []Result
	next := map[string]string{}
	asked := false
	for _, v := range views {
		r := Result{Name: v.Name, Rows: -1, Whole: WholeByDesign[v.Name]}
		switch {
		case slices.Contains(o.Skip, v.Name):
			r.Not = Skipped
		case v.AdminOnly && !admin:
			r.Not = NotAllowed
		case v.Path == "":
			r.Not = NoData
		}
		if r.Not != NotSkipped {
			results = append(results, r)
			continue
		}
		if asked && o.Pause > 0 {
			select {
			case <-time.After(o.Pause):
			case <-ctx.Done():
				return results, ctx.Err()
			}
		}
		asked = true
		if o.Started != nil {
			o.Started(v.Name)
		}
		path := strings.Replace(v.Path, "@next", url.QueryEscape(next[v.After]), 1)
		r.Best = time.Hour
		for try := 0; try <= o.Tries; try++ {
			took, status, body, err := ask(ctx, handler, user, path, o.Timeout)
			if err != nil {
				return results, fmt.Errorf("%q: %w", v.Name, err)
			}
			if status != http.StatusOK {
				return results, fmt.Errorf("%q answered %d", v.Name, status)
			}
			if try == 0 {
				continue
			}
			r.Best = min(r.Best, took)
			if try == 1 {
				r.Rows = rowsOf(body, v.Feed)
				var cursor struct {
					NextCursor string `json:"nextCursor"`
				}
				if !v.Feed && json.Unmarshal(body, &cursor) == nil {
					next[v.Name] = cursor.NextCursor
				}
			}
		}
		results = append(results, r)
	}
	return results, nil
}

func ask(ctx context.Context, handler http.Handler, user auth.User, path string, timeout time.Duration) (time.Duration, int, []byte, error) {
	ctx, cancel := context.WithTimeout(httpapi.WithUser(ctx, user), timeout)
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, httpapi.APIPrefix+path, nil)
	rec := httptest.NewRecorder()
	began := time.Now()
	handler.ServeHTTP(rec, req)
	took := time.Since(began)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return took, 0, nil, fmt.Errorf("no answer within %s", timeout)
	}
	if ctx.Err() != nil {
		return took, 0, nil, ctx.Err()
	}
	return took, rec.Code, rec.Body.Bytes(), nil
}

// rowsOf is the length of the longest list in an answer: of an Atom feed
// its entries, -1 where there is no list.
func rowsOf(body []byte, feed bool) int {
	if feed {
		return bytes.Count(body, []byte("<entry"))
	}
	var answer any
	if json.Unmarshal(body, &answer) != nil {
		return -1
	}
	rows := -1
	switch v := answer.(type) {
	case []any:
		rows = len(v)
	case map[string]any:
		for _, field := range v {
			if list, ok := field.([]any); ok {
				rows = max(rows, len(list))
			}
		}
	}
	return rows
}
