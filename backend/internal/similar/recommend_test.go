package similar

import (
	"testing"

	"github.com/google/uuid"
)

func axis(n int, tilt float32) []float32 {
	v := make([]float32, 8)
	v[n] = 1
	v[7] = tilt
	return v
}

func book(weight float64, n int, tilt float32) liked {
	return liked{id: uuid.New(), weight: weight, metadata: axis(n, tilt), content: axis(n, tilt)}
}

func TestAProfileNeedsThreeBooksThatCountForIt(t *testing.T) {
	if _, ok := buildProfile([]liked{book(1, 0, 0), book(1, 0, 0.1), book(0, 0, 0.2), book(-0.3, 1, 0)}, DefaultTaste); ok {
		t.Error("two books that count make a profile")
	}
	if _, ok := buildProfile([]liked{book(1, 0, 0), book(0.7, 0, 0.1), book(0.4, 0, 0.2)}, DefaultTaste); !ok {
		t.Error("three books make none")
	}
}

func TestTwoTastesAreTwoCentresAndNotOneGreyMean(t *testing.T) {
	var books []liked
	for i := range 4 {
		books = append(books, book(1, 0, float32(i)/10), book(0.7, 1, float32(i)/10))
	}
	p, ok := buildProfile(books, DefaultTaste)
	if !ok || p.Centres() != 2 {
		t.Fatalf("centres = %d", p.Centres())
	}
	for _, c := range p.centres {
		first := c.members[0].metadata
		for _, m := range c.members {
			if Cosine(first, m.metadata) < 0.9 {
				t.Errorf("a centre mixes the tastes")
			}
		}
	}
	one := DefaultTaste
	one.Centres = 1
	if p, _ := buildProfile(books, one); p.Centres() != 1 {
		t.Errorf("one centre asked: %d", p.Centres())
	}
}

func TestACentreOfOneBookIsJoinedToAnother(t *testing.T) {
	books := []liked{book(1, 0, 0), book(1, 0, 0.1), book(1, 0, 0.2), book(1, 0, 0.3), book(1, 1, 0)}
	p, _ := buildProfile(books, DefaultTaste)
	for _, c := range p.centres {
		if len(c.members) < 2 {
			t.Errorf("a centre of %d", len(c.members))
		}
	}
}

func TestBooksThatCountAgainstPullTheirCentreAwayFromThem(t *testing.T) {
	good := []liked{book(1, 0, 0), book(1, 0, 0.1), book(1, 0, 0.2)}
	disliked := book(-0.3, 0, 0.9)
	with, _ := buildProfile(append(good[:3:3], disliked), Taste{Five: 1, Centres: 1})
	without, _ := buildProfile(good, Taste{Five: 1, Centres: 1})
	probe := axis(0, 0.9)
	if Cosine(with.centres[0].metadata, probe) >= Cosine(without.centres[0].metadata, probe) {
		t.Error("a disliked book did not push the centre away")
	}
}

func TestTheWeightOfABookFollowsItsStars(t *testing.T) {
	five, four, one := int16(5), int16(4), int16(1)
	tt := DefaultTaste
	for _, c := range []struct {
		status string
		rating *int16
		want   float64
	}{{"completed", &five, 1}, {"reading", &four, 0.7}, {"completed", nil, 0.4}, {"reading", nil, 0}, {"completed", &one, 0}} {
		if got := tt.weight(c.status, c.rating); got != c.want {
			t.Errorf("%s %v = %v, want %v", c.status, c.rating, got, c.want)
		}
	}
}
