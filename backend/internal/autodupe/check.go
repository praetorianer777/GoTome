package autodupe

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/praetorianer777/gotome/backend/internal/dedupe"
	"github.com/praetorianer777/gotome/backend/internal/library"
)

// ClassReport is what the rules would do with a sample of the open pairs of
// one class. It holds counts and the names of rules and fields, never a
// title or an author: the check runs on someone's own library.
type ClassReport struct {
	Class string `json:"class"`
	// Pairs is how many open pairs the class has, Sampled how many were
	// looked at.
	Pairs   int `json:"pairs"`
	Sampled int `json:"sampled"`
	// Merged, Linked and Left count what the rules would do.
	Merged int `json:"merged"`
	Linked int `json:"linked"`
	Left   int `json:"left"`
	// Trashing counts the merges that trash a file.
	Trashing int `json:"trashing"`
	// Kept counts, per reason, which book would survive.
	Kept map[string]int `json:"kept,omitempty"`
	// Taken counts, per field, the merges that take it from the other book.
	Taken map[string]int `json:"taken,omitempty"`
	// Differing counts, per field, the merges that find a conflict there.
	Differing map[string]int `json:"differing,omitempty"`
	// Reasons counts the pairs left, per reason.
	Reasons map[string]int `json:"reasons,omitempty"`
	// Jaccard is the lowest and the highest Jaccard figure sampled.
	JaccardLow  float64 `json:"jaccardLow,omitempty"`
	JaccardHigh float64 `json:"jaccardHigh,omitempty"`
}

// Report is the check of every class.
type Report struct {
	OnConflict string        `json:"onConflict"`
	Classes    []ClassReport `json:"classes"`
}

func sampleKey(id uuid.UUID) [32]byte { return sha256.Sum256(id[:]) }

// Check looks at a sample of at most n open pairs per class, chosen by a
// hash of the pair's ID, and says what the rules would do with each,
// changing nothing.
func (s *Service) Check(ctx context.Context, scope library.Scope, n int, mergeAgreeing bool) (Report, error) {
	byClass := map[string][]Pair{}
	err := s.Pairs(ctx, scope, nil, nil, func(p Pair) (bool, error) {
		byClass[p.Class.Class] = append(byClass[p.Class.Class], p)
		return true, nil
	})
	if err != nil {
		return Report{}, err
	}
	out := Report{OnConflict: "skip"}
	if mergeAgreeing {
		out.OnConflict = MergeAgreeing
	}
	for _, class := range dedupe.AutoClasses {
		pairs := byClass[class]
		r := ClassReport{Class: class, Pairs: len(pairs), Kept: map[string]int{}, Taken: map[string]int{}, Differing: map[string]int{}, Reasons: map[string]int{}}
		slices.SortFunc(pairs, func(a, b Pair) int {
			ka, kb := sampleKey(a.ID), sampleKey(b.ID)
			return strings.Compare(string(ka[:]), string(kb[:]))
		})
		if len(pairs) > n {
			pairs = pairs[:n]
		}
		for _, p := range pairs {
			plan, ok, err := s.PlanFor(ctx, scope, mergeAgreeing, p)
			if err != nil {
				return out, err
			}
			if !ok {
				continue
			}
			r.Sampled++
			if o := p.Class.Overlap; o != nil {
				if r.JaccardLow == 0 || o.Jaccard < r.JaccardLow {
					r.JaccardLow = o.Jaccard
				}
				r.JaccardHigh = max(r.JaccardHigh, o.Jaccard)
			}
			switch plan.Action {
			case Merge:
				r.Merged++
				r.Kept[plan.Why]++
				if len(plan.Copies) > 0 {
					r.Trashing++
				}
				for _, f := range plan.Fields.Take {
					r.Taken[f]++
				}
				for _, f := range plan.Fields.Conflicts {
					r.Differing[f]++
				}
			case Link:
				r.Linked++
			default:
				r.Left++
				r.Reasons[plan.Left]++
				for _, f := range plan.Fields.Conflicts {
					r.Differing[f]++
				}
			}
		}
		out.Classes = append(out.Classes, r)
	}
	return out, nil
}

func lines(w io.Writer, indent string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return counts[keys[i]] > counts[keys[j]] || counts[keys[i]] == counts[keys[j]] && keys[i] < keys[j]
	})
	for _, k := range keys {
		fmt.Fprintf(w, "%s%-28s %d\n", indent, k, counts[k])
	}
}

// PrintCheck writes the report for a person to judge the rules by.
func PrintCheck(w io.Writer, r Report) {
	fmt.Fprintf(w, "What the duplicate rules would do (a pair with a field in conflict: %s)\n", r.OnConflict)
	for _, c := range r.Classes {
		fmt.Fprintf(w, "\n%s: %d open, %d looked at\n", c.Class, c.Pairs, c.Sampled)
		if c.Sampled == 0 {
			continue
		}
		if c.JaccardHigh > 0 {
			fmt.Fprintf(w, "  shared text (Jaccard) from %.2f to %.2f\n", c.JaccardLow, c.JaccardHigh)
		}
		fmt.Fprintf(w, "  merged %d (of which %d trash a copy), linked %d, left for a person %d\n", c.Merged, c.Trashing, c.Linked, c.Left)
		for _, part := range []struct {
			title string
			data  map[string]int
		}{{"survivor chosen because", c.Kept}, {"fields taken from the other", c.Taken}, {"fields that differ", c.Differing}, {"left because", c.Reasons}} {
			if len(part.data) > 0 {
				fmt.Fprintf(w, "  %s:\n", part.title)
				lines(w, "    ", part.data)
			}
		}
	}
}
