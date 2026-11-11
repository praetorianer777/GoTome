package dedupe

import (
	"fmt"
	"regexp"
	"strconv"
)

// The classes an open pair falls in by its evidence. Only the first
// two merge and the next two link; the rest is for a person.
const (
	// ClassSameText is a file of each with the same bytes or the same text.
	ClassSameText = "same_text"
	// ClassOverlapHigh is shared text with a Jaccard of 0.95 or more.
	ClassOverlapHigh = "overlap_high"
	// ClassOverlapMid is shared text with a Jaccard from 0.8 to under 0.95.
	ClassOverlapMid = "overlap_mid"
	// ClassContained is one book holding the other's text whole (0.95 or
	// more of it) while the two are far from one text (Jaccard under 0.5).
	ClassContained = "contained"
	// ClassPartInside is one holding 0.8 to under 0.95 of the other.
	ClassPartInside = "part_inside"
	// ClassPerson is every other pair.
	ClassPerson = "person"
)

// AutoClasses are the classes the rules may handle by themselves, in the
// order a page lists them.
var AutoClasses = []string{ClassSameText, ClassOverlapHigh, ClassOverlapMid, ClassContained, ClassPartInside}

// Thresholds of the classes, from the figures of the overlap evidence.
const (
	JaccardMerge         = 0.95
	JaccardMergeMeasured = 0.8
	ContainedWhole       = 0.95
	ContainedPart        = 0.8
	JaccardApart         = 0.5
)

// Merges says whether pairs of the class are made one book.
func Merges(class string) bool {
	return class == ClassSameText || class == ClassOverlapHigh || class == ClassOverlapMid
}

// Links says whether pairs of the class stay two books, linked.
func Links(class string) bool { return class == ClassContained || class == ClassPartInside }

// Found is one piece of a pair's evidence as stored.
type Found struct {
	Kind   string
	Detail string
}

// Overlap is the figures of overlap evidence: the Jaccard estimate and how
// much of each book the other holds, in the order of the pair's books.
type Overlap struct {
	Jaccard, AInB, BInA float64
}

var overlapDetail = regexp.MustCompile(`^jaccard=([0-9.]+) a_in_b=([0-9.]+) b_in_a=([0-9.]+)$`)

// ParseOverlap reads the detail of overlap evidence.
func ParseOverlap(detail string) (Overlap, error) {
	m := overlapDetail.FindStringSubmatch(detail)
	if m == nil {
		return Overlap{}, fmt.Errorf("not the detail of overlap evidence: %q", detail)
	}
	var f [3]float64
	for i := range f {
		v, err := strconv.ParseFloat(m[i+1], 64)
		if err != nil {
			return Overlap{}, err
		}
		f[i] = v
	}
	return Overlap{Jaccard: f[0], AInB: f[1], BInA: f[2]}, nil
}

// Holder says which book of the pair holds the other's text, "a" or "b",
// or "" where neither holds it clearly more.
func (o Overlap) Holder() string {
	switch {
	case o.AInB > o.BInA:
		return "b"
	case o.BInA > o.AInB:
		return "a"
	}
	return ""
}

// Classification is the class of a pair and what it was worked out from.
type Classification struct {
	Class string
	// Overlap is the figures the class rests on, if it rests on any.
	Overlap *Overlap
	// Holder is, for a contained pair, the book that holds the other.
	Holder string
}

// Classify works out the class of a pair from its stored evidence. Shared
// bytes or content make a pair the same text, whatever else is known. Else
// the strongest overlap decides: the Jaccard from 0.8 up, and under 0.5 how
// much of the smaller the larger holds. A pair whose evidence is more than
// overlap is judged by the overlap alone only where that merges: any other
// evidence of identity (an ISBN, the same title and author) only agrees
// with it. Linking needs overlap to be all there is, as the pair's
// contained flag does, since an ISBN says the two are editions of one
// book. Pairs of no clear figure are a person's.
func Classify(evidence []Found) Classification {
	var overlap *Overlap
	other := false
	for _, e := range evidence {
		switch e.Kind {
		case EvidenceSHA256, EvidenceContent:
			return Classification{Class: ClassSameText}
		case EvidenceOverlap:
			o, err := ParseOverlap(e.Detail)
			if err != nil {
				return Classification{Class: ClassPerson}
			}
			if overlap == nil || o.Jaccard > overlap.Jaccard {
				overlap = &o
			}
		default:
			other = true
		}
	}
	if overlap == nil {
		return Classification{Class: ClassPerson}
	}
	c := Classification{Class: ClassPerson, Overlap: overlap}
	switch {
	case overlap.Jaccard >= JaccardMerge:
		c.Class = ClassOverlapHigh
	case overlap.Jaccard >= JaccardMergeMeasured:
		c.Class = ClassOverlapMid
	case overlap.Jaccard < JaccardApart && !other:
		held := max(overlap.AInB, overlap.BInA)
		switch {
		case held >= ContainedWhole:
			c.Class = ClassContained
		case held >= ContainedPart:
			c.Class = ClassPartInside
		}
		if c.Class != ClassPerson {
			c.Holder = overlap.Holder()
			if c.Holder == "" {
				c.Class = ClassPerson
			}
		}
	}
	return c
}
