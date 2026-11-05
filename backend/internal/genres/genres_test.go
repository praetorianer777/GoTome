package genres

import (
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/settings"
)

func TestTheListHasUniqueKeysNamesAndDescriptions(t *testing.T) {
	seen := map[string]bool{}
	key := regexp.MustCompile(`^[a-z]+(-[a-z]+)*$`)
	for _, g := range List {
		if !key.MatchString(g.Key) || seen[g.Key] {
			t.Errorf("key %q is not a unique key of lower-case words", g.Key)
		}
		seen[g.Key] = true
		if g.English == "" || g.German == "" || g.About == "" || g.Über == "" {
			t.Errorf("%s lacks a name or a description", g.Key)
		}
	}
	for genre := range Spellings {
		if _, ok := Get(genre); !ok {
			t.Errorf("Spellings names %q, which is not on the list", genre)
		}
	}
}

func TestTheSettingTakesEveryMode(t *testing.T) {
	if !slices.Equal(Modes, settings.GenreModes) {
		t.Errorf("modes %v, the setting takes %v", Modes, settings.GenreModes)
	}
}

func TestTagsMapOntoGenres(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want []string
	}{
		{"Krimi", []string{"crime"}},
		{"KRIMIS & THRILLER", []string{"crime", "thriller"}},
		{"Krimi-Thriller", []string{"crime", "thriller"}},
		{"Kriminalroman", []string{"crime"}},
		{"Belletristik/Gegenwartsliteratur (ab 1945)", []string{"contemporary"}},
		{"Belletristik/Kriminalromane", []string{"crime"}},
		{"Fiction / Science Fiction / General", []string{"science-fiction"}},
		{"Juvenile Fiction / Fantasy & Magic", []string{"children", "fantasy"}},
		{"Science Fiction, Fantasy", []string{"fantasy", "science-fiction"}},
		{"Historische Romane, Erzählungen", []string{"historical"}},
		{"Science-Fiction", []string{"science-fiction"}},
		{"Julia", []string{"romance"}},
		{"Historical RomanHefte", []string{"historical", "romance"}},
		{"Historischer Krimi", []string{"crime", "historical"}},
		{"Belletristik/Historische Kriminalromane", []string{"crime", "historical"}},
		{"Scheibenwelt", []string{"fantasy", "humour"}},
		{"Dämonenkiller", []string{"horror"}},
		{"Vampir Horror Roman", []string{"horror"}},
		{"Rettungskreuzer Ikarus", []string{"science-fiction"}},
		{"Jugend", []string{"young-adult"}},
		{"Sex", []string{"erotica"}},
		{"General", nil},
		{"Roman", nil},
		{"Belletristik", nil},
		{"Fiction", nil},
		{"Sachbuch", nil},
	} {
		got := FromTag(c.tag)
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: %v, want %v", c.tag, got, c.want)
		}
	}
	got := FromTags([]string{"Thriller", "Krimi", "Krimi"})
	if !slices.Equal(got, []string{"crime", "thriller"}) {
		t.Errorf("tags give %v, in the list's order want crime, thriller", got)
	}
}

func TestKnownGenresLeaveOutWhatAPersonRejected(t *testing.T) {
	got := knownGenres([]string{"Krimi", "Thriller"}, []string{"horror"}, []string{"thriller"})
	if !slices.Equal(got, []string{"crime", "horror"}) {
		t.Errorf("known %v", got)
	}
}

func axis(i int) []float32 {
	v := make([]float32, 8)
	v[i] = 1
	return v
}

func TestMarginsPickTheGenresNearestTheBook(t *testing.T) {
	vecs := make([][]float32, len(List))
	for i := range vecs {
		vecs[i] = axis(7)
	}
	crime, thriller := slices.Index(Keys(), "crime"), slices.Index(Keys(), "thriller")
	vecs[crime], vecs[thriller] = axis(0), axis(1)
	book := Vectors{Content: []float32{1, 0.5, 0, 0, 0, 0, 0, 0}}
	m := Margins(book, vecs)
	if m[crime] <= m[thriller] || m[thriller] <= 0 {
		t.Fatalf("margins crime %v, thriller %v", m[crime], m[thriller])
	}
	var sum float64
	for _, x := range m {
		sum += x
	}
	if math.Abs(sum) > 1e-9 {
		t.Errorf("margins sum to %v, not 0", sum)
	}
	got := pickMargins(m, Genre.threshold, nil, MaxGenres)
	if len(got) != 2 || got[0].Genre != "crime" || got[1].Genre != "thriller" {
		t.Errorf("picked %v", got)
	}
	if got := pickMargins(m, Genre.threshold, []string{"crime"}, 1); len(got) != 1 || got[0].Genre != "thriller" {
		t.Errorf("picked past what the book has %v", got)
	}
	if Margins(Vectors{}, vecs) != nil {
		t.Error("a book without vectors has margins")
	}
}

func TestNeighboursNeedEnoughWithGenres(t *testing.T) {
	near := []Near{
		{Score: 0.9, Genres: []string{"crime"}},
		{Score: 0.8},
		{Score: 0.8, Genres: []string{"crime", "thriller"}},
	}
	if Shares(near) != nil {
		t.Error("two neighbours with genres gave shares")
	}
	near = append(near, Near{Score: 0.7, Genres: []string{"fantasy"}})
	shares := Shares(near)
	if math.Abs(shares["crime"]-1.7/2.4) > 1e-9 || math.Abs(shares["fantasy"]-0.7/2.4) > 1e-9 {
		t.Errorf("shares %v", shares)
	}
	got := pickShares(shares, NeighbourShare, nil, MaxGenres)
	if len(got) != 1 || got[0].Genre != "crime" {
		t.Errorf("picked %v", got)
	}
}

func TestACheckMeasuresPrecisionAndRecall(t *testing.T) {
	sample := []sampled{
		{truth: []string{"crime"}, scores: map[string]map[string]float64{MethodVector: {"crime": 0.2, "horror": 0.1}}},
		{truth: []string{"horror"}, scores: map[string]map[string]float64{MethodVector: {"crime": 0.15, "horror": 0.01}}},
	}
	p := point(sample, MethodVector, func(string) float64 { return 0.1 })
	// Given crime and horror, then crime: one right of three, one of the
	// two wanted.
	if math.Abs(p.Precision-1.0/3) > 1e-9 || p.Recall != 0.5 || p.Covered != 1 {
		t.Errorf("point %+v", p)
	}
	r := measure(sample, MethodVector, 0.9)
	var crime GenreResult
	for _, g := range r.Genres {
		if g.Genre == "crime" {
			crime = g
		}
	}
	if !crime.Reached || crime.Precision != 1 || crime.Recall != 1 || crime.Threshold <= 0.15 {
		t.Errorf("crime %+v", crime)
	}
}

func TestCombinedAsksTheVectorsOnlyWithoutNeighbours(t *testing.T) {
	vector := map[string]float64{"horror": 0.3}
	both := sampled{scores: map[string]map[string]float64{MethodVector: vector, MethodNeighbours: {"crime": 0.6}}}
	if got := predict(both, MethodCombined, nil); !slices.Equal(got, []string{"crime"}) {
		t.Errorf("with neighbours, combined gives %v", got)
	}
	unsure := sampled{scores: map[string]map[string]float64{MethodVector: vector, MethodNeighbours: {"crime": 0.1}}}
	if got := predict(unsure, MethodCombined, nil); len(got) != 0 {
		t.Errorf("with neighbours that agree on nothing, combined gives %v", got)
	}
	alone := sampled{scores: map[string]map[string]float64{MethodVector: vector}}
	if got := predict(alone, MethodCombined, nil); !slices.Equal(got, []string{"horror"}) {
		t.Errorf("without neighbours, combined gives %v", got)
	}
}

// The web app names the genres in its translations and lists their keys;
// they must say what the list says.
func TestTheWebAppNamesTheGenresAsTheListDoes(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	en, de := read("../../../web/src/i18n/en.ts"), read("../../../web/src/i18n/de.ts")
	list := read("../../../web/src/books/genres.ts")
	for _, g := range List {
		for _, c := range []struct{ file, text, name string }{{"en.ts", en, g.English}, {"de.ts", de, g.German}} {
			want := `"genre.` + g.Key + `": "` + c.name + `"`
			if !strings.Contains(c.text, want) {
				t.Errorf("%s lacks %s", c.file, want)
			}
		}
		if !strings.Contains(list, `"`+g.Key+`"`) {
			t.Errorf("books/genres.ts lacks %q", g.Key)
		}
	}
}
