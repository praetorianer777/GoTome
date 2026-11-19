package genres

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTheEnsembleIsJudgedOnlyByModelsThatHadNotSeenTheBook(t *testing.T) {
	var books []*loaded
	for i, b := range synthetic(600) {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(b.ID))
		lb := &loaded{id: id, truth: b.Labels, vectors: Vectors{Content: b.Content}}
		if i < 200 {
			lb.one = &sampled{truth: b.Labels, scores: map[string]map[string]float64{}, given: map[string][]string{}}
		}
		books = append(books, lb)
	}
	trained, genreCount := judgeOutOfFold(books, "m")
	if genreCount != 3 || trained < 400 || trained > 500 {
		t.Fatalf("trained on %d books, %d genres: a fold's ensemble sees four fifths", trained, genreCount)
	}
	var sample []sampled
	for _, b := range books[:200] {
		sample = append(sample, *b.one)
	}
	r := measure(sample, MethodEnsemble, 0.8)
	if r.Current.Precision < 0.8 || r.Current.Recall < 0.3 || r.Current.Covered < 0.5 {
		t.Errorf("ensemble at its thresholds: %+v", r.Current)
	}
	for _, d := range r.Decided {
		if d.Genre == "poetry" && d.Precision > 0 {
			t.Errorf("poetry is chance and should abstain: %+v", d)
		}
	}
	content := measure(sample, inputMethod("content"), 0.8)
	if len(content.Sweep) == 0 || content.Current.Precision == 0 {
		t.Errorf("the content input: %+v", content)
	}
	if words := measure(sample, inputMethod("words"), 0.8); words.Current.Covered != 0 {
		t.Errorf("no book has words, yet %+v", words)
	}
}

func TestACheckReportHoldsGenreKeysAndNumbersAlone(t *testing.T) {
	sample := []sampled{{
		truth:  []string{"crime"},
		scores: map[string]map[string]float64{MethodEnsemble: {"crime": 0.9}},
		given:  map[string][]string{MethodEnsemble: {"crime"}},
	}}
	r := CheckReport{
		Model: "m", Sample: 1, Trained: 10, TrainedGenres: 1, PerGenre: map[string]int{"crime": 1},
		Methods: []MethodReport{measure(sample, MethodEnsemble, 0.8)},
	}
	var out bytes.Buffer
	PrintCheck(&out, r, 0.8)
	if !strings.Contains(out.String(), "crime") || !strings.Contains(out.String(), "== ensemble") {
		t.Fatalf("report:\n%s", out.String())
	}
}
