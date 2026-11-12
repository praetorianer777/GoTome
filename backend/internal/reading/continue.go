package reading

import (
	"math"
	"sort"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

const (
	// MinGain is how much further, as a share of the whole, the other
	// medium must be before going on there is offered: a mapped place is
	// never exact, and a few pages are no reason to switch.
	MinGain = 0.01
	// minAnchors is how many chapters both media must name alike before
	// the chapters refine the mapping; one match proves nothing.
	minAnchors = 2
)

// Landmark is a chapter of one medium: its title and the share of the whole
// that lies before it.
type Landmark struct {
	Title string
	Start float64
}

// Offer is a place in one medium to go on at, because the other medium is
// further along.
type Offer struct {
	// Fraction is the place as a share of the whole.
	Fraction float64
	// Chapter names the chapter it is in, only when the chapters of both
	// media were matched (Refined); otherwise a guess by share would be
	// dressed up as a fact.
	Chapter string
	// Refined says that chapters both media name alike placed it, and not
	// the share alone.
	Refined bool
}

// Map carries a place, as a share of the whole, from one medium into
// another. Chapters that both name alike (compared by catalog.Key, each
// title once in each, in the same order) are anchors; between two anchors
// the place keeps its share of the way, so the same chapter at the same
// share inside it. With fewer than two anchors, or none around the place,
// the share of the whole is all there is and refined is false.
func Map(fraction float64, from, to []Landmark) (mapped float64, refined bool) {
	fraction = math.Min(math.Max(fraction, 0), 1)
	anchors := anchorsOf(from, to)
	if len(anchors) < minAnchors {
		return fraction, false
	}
	// The ends of the book are anchors too, so that what lies before the
	// first chapter and after the last is spread over what the other
	// medium has there.
	points := append([][2]float64{{0, 0}}, anchors...)
	points = append(points, [2]float64{1, 1})
	i := sort.Search(len(points), func(i int) bool { return points[i][0] > fraction }) - 1
	if i < 0 || i >= len(points)-1 {
		return fraction, false
	}
	lo, hi := points[i], points[i+1]
	if hi[0] <= lo[0] {
		return lo[1], true
	}
	return lo[1] + (fraction-lo[0])/(hi[0]-lo[0])*(hi[1]-lo[1]), true
}

// anchorsOf pairs the starts of the chapters that carry the same title in
// both lists, in the order they lie in both: a title that stands twice in
// either list, or that the other medium has in another order, is left out.
func anchorsOf(from, to []Landmark) [][2]float64 {
	count := func(list []Landmark) map[string]int {
		n := map[string]int{}
		for _, l := range list {
			n[catalog.Key(l.Title)]++
		}
		return n
	}
	inFrom, inTo := count(from), count(to)
	toStart := map[string]float64{}
	for _, l := range to {
		toStart[catalog.Key(l.Title)] = l.Start
	}
	var out [][2]float64
	for _, l := range from {
		key := catalog.Key(l.Title)
		if key == "" || inFrom[key] != 1 || inTo[key] != 1 {
			continue
		}
		start, ok := toStart[key]
		if !ok {
			continue
		}
		// Both lists run from the start to the end, so a pair that does
		// not lie beyond the one before it in both is out of order.
		if n := len(out); n > 0 && (l.Start <= out[n-1][0] || start <= out[n-1][1]) {
			continue
		}
		out = append(out, [2]float64{l.Start, start})
	}
	return out
}

// LandmarkAt names the chapter a place lies in: the last whose start is not
// after it, or "" before the first.
func LandmarkAt(list []Landmark, fraction float64) string {
	title := ""
	for _, l := range list {
		if l.Start <= fraction {
			title = l.Title
		}
	}
	return title
}

// Sides is what Continue works from: the two saved places, and the
// chapters of each medium as shares of the whole.
type Sides struct {
	// Ebook and Audio are the saved places; nil where there is none.
	Ebook, Audio *Progress
	// AudioMS is the length of the whole audiobook; zero when there is no
	// audio to play, or not all of it is known.
	AudioMS int64
	// HasEbook says the book has a file the browser's reader opens.
	HasEbook      bool
	EbookChapters []Landmark
	AudioChapters []Landmark
}

// Offers are the places to go on at, one per medium; nil where that medium
// is not the one behind.
type Offers struct {
	Ebook, Audio *Offer
}

// audioFraction is where the audio position lies in the whole. The
// millisecond is exact where the timeline is the one it was written on.
func audioFraction(p *Progress, durationMS int64) float64 {
	if p.PositionMS != nil && durationMS > 0 {
		return math.Min(float64(*p.PositionMS)/float64(durationMS), 1)
	}
	return p.Fraction
}

// Continue decides what to offer. The medium that is further along, by the
// place it reached as a share of the whole, is the one to go on from; the
// other gets an offer to continue at the mapped place when that lies at
// least MinGain beyond its own, or when it has none. Which was written
// last does not matter: someone who read ahead and then listened is still
// further along in the text. A place at the end (reading.End) is a book
// finished, and is offered nowhere. Nothing is ever moved here: an offer
// is only what a person may choose.
func Continue(s Sides) Offers {
	var out Offers
	canListen := s.AudioMS > 0
	if s.Audio != nil && s.HasEbook && canListen {
		f := audioFraction(s.Audio, s.AudioMS)
		if f < End {
			out.Ebook = offerFor(f, s.Ebook, 0, s.AudioChapters, s.EbookChapters)
		}
	}
	if s.Ebook != nil && canListen && s.Ebook.Fraction < End {
		out.Audio = offerFor(s.Ebook.Fraction, s.Audio, s.AudioMS, s.EbookChapters, s.AudioChapters)
	}
	return out
}

// audioOr is a place's share of the whole, from its millisecond where it
// has one and the duration is known.
func audioOr(p *Progress, durationMS int64) float64 {
	if p.Medium == MediumAudio {
		return audioFraction(p, durationMS)
	}
	return p.Fraction
}

func offerFor(from float64, own *Progress, ownMS int64, fromChapters, toChapters []Landmark) *Offer {
	mapped, refined := Map(from, fromChapters, toChapters)
	have := 0.0
	if own != nil {
		have = audioOr(own, ownMS)
	}
	if mapped < MinGain || mapped < have+MinGain {
		return nil
	}
	offer := &Offer{Fraction: mapped, Refined: refined}
	if refined {
		offer.Chapter = LandmarkAt(toChapters, mapped)
	}
	return offer
}
