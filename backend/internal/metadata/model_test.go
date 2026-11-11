package metadata

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

func TestFitFindsWhatSeparatesTheRightFromTheWrong(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var examples []example
	for range 600 {
		title := rng.Float64()
		noise := rng.Float64()
		examples = append(examples, example{
			features: map[string]float64{FeatureBias: 1, FeatureTitle: title, FeatureYearNear: noise},
			right:    title+0.2*(rng.Float64()-0.5) > 0.6,
		})
	}
	w := fit(examples)
	if w[FeatureTitle] < 5 {
		t.Errorf("the title separates the two and weighs %.2f", w[FeatureTitle])
	}
	if w[FeatureTitle] < 5*abs(w[FeatureYearNear]) {
		t.Errorf("noise weighs %.2f against the title's %.2f", w[FeatureYearNear], w[FeatureTitle])
	}
	if w[FeatureBias] > -2 {
		t.Errorf("most are wrong, the bias is %.2f", w[FeatureBias])
	}
	if got := w.logLoss(examples); got > 0.35 {
		t.Errorf("log loss %.3f", got)
	}
	high := w.probability(map[string]float64{FeatureBias: 1, FeatureTitle: 1})
	low := w.probability(map[string]float64{FeatureBias: 1, FeatureTitle: 0.1})
	if high < 0.9 || low > 0.1 {
		t.Errorf("probabilities %.2f and %.2f", high, low)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestFitSurvivesFeaturesThatNeverVaryAndPerfectSeparation(t *testing.T) {
	var examples []example
	for i := range 40 {
		examples = append(examples, example{features: map[string]float64{FeatureBias: 1, FeatureTitleExact: 1, FeatureTitle: float64(i % 2)}, right: i%2 == 1})
	}
	w := fit(examples)
	if err := w.Valid(); err != nil || w[FeatureTitle] <= 0 {
		t.Errorf("%v %v", w, err)
	}
	if len(fit(nil)) != 0 {
		t.Error("nothing to fit gives weights")
	}
}

func TestFeaturesNameWhatTheHandSetScoreCompares(t *testing.T) {
	book, r := buddenbrooks()
	f := features(book, r, scoreInput{})
	for _, name := range []string{FeatureBias, FeatureTitle, FeatureTitleExact, FeatureAuthors, FeatureYearSilent, FeatureLanguageSame,
		FeaturePagesNear, FeaturePublisherSame, FeatureSeriesSame, FeaturePositionSame, FeatureFirstInList} {
		if f[name] == 0 {
			t.Errorf("%s is not set: %v", name, f)
		}
	}
	for _, name := range []string{FeatureAuthorsSilent, FeatureLanguageOther, FeaturePagesFar, FeatureSeriesDiff, FeatureLateInList, FeatureDescription} {
		if f[name] != 0 {
			t.Errorf("%s is set: %v", name, f)
		}
	}

	r.Provider, r.Language, r.Series, r.SeriesIndex = "dnb", "en", "Andere Reihe", nil
	f = features(book, r, scoreInput{rank: 3, cosine: ptr(0.9)})
	if f[FeatureLanguageOther] != 1 || f[FeatureSeriesDiff] != 1 || f["provider:dnb"] != 1 || f[FeatureLateInList] < 1.3 || f[FeatureFirstInList] != 0 {
		t.Errorf("%v", f)
	}
	if f[FeatureDescription] <= 0 {
		t.Errorf("a description nearer than usual counts for the record: %v", f)
	}

	book.Contributors = nil
	if f = features(book, r, scoreInput{}); f[FeatureAuthorsSilent] != 1 || f[FeatureAuthors] != 0 {
		t.Errorf("a book without authors says nothing about them: %v", f)
	}
}

func TestEveryFeatureTheHandSetScoreWeighsIsOneOfTheFeatures(t *testing.T) {
	for name := range handSet {
		if !knownFeatures[name] {
			t.Errorf("%s is no feature", name)
		}
	}
}

func TestALearnedScoreIsAProbabilityShortOfAnIdentifier(t *testing.T) {
	book, r := buddenbrooks()
	w := Weights{FeatureBias: 20}
	if got := learnedScorer(w)(book, r, scoreInput{}); got != bestWithoutIdentifier {
		t.Errorf("a sure record without an identifier: %v", got)
	}
	w = Weights{FeatureBias: -20}
	if got := learnedScorer(w)(book, r, scoreInput{}); got > 1e-6 {
		t.Errorf("a hopeless record: %v", got)
	}
	book.Identifiers = []catalog.BookIdentifier{{Identifier: catalog.Identifier{Type: catalog.IDISBN, Value: "9783100481160"}}}
	r.Identifiers = []catalog.Identifier{book.Identifiers[0].Identifier}
	if got := learnedScorer(w)(book, r, scoreInput{}); got != 1 {
		t.Errorf("a shared identifier: %v", got)
	}
}

func goodProfile() Profile {
	return Profile{
		Format: ProfileFormat, FeatureVersion: FeatureVersion, GotomeVersion: "0.19.0", Books: 500,
		Languages: map[string]float64{"de": 0.9, "other": 0.1}, Providers: map[string]float64{"dnb": 0.6},
		Weights: Weights{FeatureBias: -4, FeatureTitle: 5, "provider:dnb": 0.3},
	}
}

func TestAFileOfWeightsIsCheckedBeforeItIsKept(t *testing.T) {
	raw, _ := json.Marshal(goodProfile())
	if _, err := ParseProfile(raw); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Profile){
		"another version": func(p *Profile) { p.FeatureVersion = FeatureVersion + 1 },
		"another format":  func(p *Profile) { p.Format = "other" },
		"a made-up feature": func(p *Profile) {
			p.Weights["Moby Dick"] = 1
		},
		"a provider with a path":      func(p *Profile) { p.Weights["provider:/etc/passwd"] = 1 },
		"a weight that is not finite": func(p *Profile) { p.Weights[FeatureTitle] = 1e9 },
		"no weights":                  func(p *Profile) { p.Weights = nil },
		"a share over one":            func(p *Profile) { p.Languages["de"] = 2 },
	} {
		p := goodProfile()
		change(&p)
		raw, _ := json.Marshal(p)
		_, err := ParseProfile(raw)
		if err == nil {
			t.Errorf("%s was taken", name)
		}
		if name == "another version" && !errors.Is(err, ErrProfileVersion) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseProfile([]byte(`{"format":"gotome-match-weights","featureVersion":1,"title":"Emma","weights":{"bias":1}}`)); !errors.Is(err, ErrProfileInvalid) {
		t.Errorf("a field a file of weights does not have: %v", err)
	}
	if _, err := ParseProfile([]byte(`nonsense`)); !errors.Is(err, ErrProfileInvalid) {
		t.Errorf("%v", err)
	}
}

func TestAShippedProfileFitsALibraryByItsLanguages(t *testing.T) {
	if !majority("de")(map[string]float64{"de": 0.6, "en": 0.3, "": 0.1}) || majority("de")(map[string]float64{"de": 0.2, "en": 0.7}) {
		t.Error("the majority of the books that name a language")
	}
	if majority("de")(map[string]float64{"": 1}) {
		t.Error("books without a language fit nothing")
	}
	for _, s := range shippedProfiles {
		if err := s.profile.Check(); err != nil {
			t.Errorf("shipped profile %s: %v", s.name, err)
		}
	}
}
