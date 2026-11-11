package metadata

import (
	"maps"
	"math"
	"slices"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// FeatureVersion names the set of features a learned score is made of and
// what each one means. A file of weights of another version is refused;
// a feature added with the weight zero for a model that lacks it does not
// change the version.
const FeatureVersion = 1

// The features of a candidate that a learned score weighs, by name. Each
// is zero where it does not apply, so a side that says nothing about a
// part counts as the part being silent, not as a match or a mismatch.
const (
	FeatureBias            = "bias"
	FeatureTitle           = "title"
	FeatureTitleExact      = "titleExact"
	FeatureAuthors         = "authors"
	FeatureAuthorsSilent   = "authorsSilent"
	FeatureYearSame        = "yearSame"
	FeatureYearNear        = "yearNear"
	FeatureYearSilent      = "yearSilent"
	FeatureLanguageSame    = "languageSame"
	FeatureLanguageSilent  = "languageSilent"
	FeatureLanguageOther   = "languageOther"
	FeaturePagesNear       = "pagesNear"
	FeaturePagesFar        = "pagesFar"
	FeaturePublisherSame   = "publisherSame"
	FeaturePublisherDiff   = "publisherDiff"
	FeatureSeriesSame      = "seriesSame"
	FeatureSeriesDiff      = "seriesDiff"
	FeaturePositionSame    = "positionSame"
	FeaturePositionDiff    = "positionDiff"
	FeatureMediumSame      = "mediumSame"
	FeatureMediumDiff      = "mediumDiff"
	FeatureFirstInList     = "firstInList"
	FeatureLateInList      = "lateInList"
	FeatureDescription     = "description"
	featureProviderPrefix  = "provider:"
	descriptionCentre      = 0.8
	descriptionCentreWidth = 0.1
)

// handSet are the weights of Score as far as they are sums of the
// features: what the learned weights are shown beside. The language
// mismatch is a factor there, not a term, and has none.
var handSet = map[string]float64{
	FeatureTitle:          weightTitle,
	FeatureAuthors:        weightAuthors,
	FeatureAuthorsSilent:  weightAuthors * neutral,
	FeatureYearSame:       weightYear,
	FeatureYearNear:       weightYear * 0.5,
	FeatureYearSilent:     weightYear * neutral,
	FeatureLanguageSame:   weightLanguage,
	FeatureLanguageSilent: weightLanguage * neutral,
	FeaturePagesNear:      pagesNear,
	FeaturePagesFar:       pagesFar,
	FeaturePublisherSame:  publisherSame,
	FeaturePublisherDiff:  publisherDiff,
	FeatureSeriesSame:     seriesSame,
	FeatureSeriesDiff:     seriesDiff,
	FeaturePositionSame:   positionSame,
	FeaturePositionDiff:   positionDiff,
	FeatureMediumSame:     mediumSame,
	FeatureMediumDiff:     mediumDiff,
}

// HandSetWeights are the hand-set score's weights per feature, where it
// has one.
func HandSetWeights() map[string]float64 { return maps.Clone(handSet) }

// scoreInput is what a score knows of a candidate besides the record and
// the book: its place in its provider's answer, and how near its
// description is to the book's text, where that was worked out.
type scoreInput struct {
	rank   int
	cosine *float64
}

// features are the nonzero features of a record against a book.
func features(book catalog.Book, r Record, in scoreInput) map[string]float64 {
	f := map[string]float64{FeatureBias: 1}
	set := func(name string, v float64) {
		if v != 0 {
			f[name] = v
		}
	}
	title := titleSimilarity(book, r)
	set(FeatureTitle, title)
	if title == 1 {
		f[FeatureTitleExact] = 1
	}
	if len(book.Authors()) == 0 || !hasAuthors(r) {
		f[FeatureAuthorsSilent] = 1
	} else {
		set(FeatureAuthors, authorsMatch(book, r))
	}
	switch years := yearsMatch(book.Published(), r.Published); {
	case years == neutral && !yearsKnown(book.Published(), r.Published):
		f[FeatureYearSilent] = 1
	case years == 1:
		f[FeatureYearSame] = 1
	case years == 0.5:
		f[FeatureYearNear] = 1
	}
	switch {
	case book.Language == "" || r.Language == "":
		f[FeatureLanguageSilent] = 1
	case languagesMatch(book.Language, r.Language) == 1:
		f[FeatureLanguageSame] = 1
	default:
		f[FeatureLanguageOther] = 1
	}
	switch pagesAdjust(book.PageCount, r.PageCount) {
	case pagesNear:
		f[FeaturePagesNear] = 1
	case pagesFar:
		f[FeaturePagesFar] = 1
	}
	switch publisherAdjust(book.Publisher, r.Publisher) {
	case publisherSame:
		f[FeaturePublisherSame] = 1
	case publisherDiff:
		f[FeaturePublisherDiff] = 1
	}
	same, diff, posSame, posDiff := seriesParts(book, r)
	for name, on := range map[string]bool{
		FeatureSeriesSame: same, FeatureSeriesDiff: diff, FeaturePositionSame: posSame, FeaturePositionDiff: posDiff,
	} {
		if on {
			f[name] = 1
		}
	}
	switch mediumAdjust(book, r) {
	case mediumSame:
		f[FeatureMediumSame] = 1
	case mediumDiff:
		f[FeatureMediumDiff] = 1
	}
	if in.rank == 0 {
		f[FeatureFirstInList] = 1
	} else {
		f[FeatureLateInList] = math.Log1p(float64(in.rank))
	}
	if r.Provider != "" {
		f[featureProviderPrefix+r.Provider] = 1
	}
	if in.cosine != nil {
		f[FeatureDescription] = (*in.cosine - descriptionCentre) / descriptionCentreWidth
	}
	return f
}

func hasAuthors(r Record) bool {
	return slices.ContainsFunc(r.Contributors, func(c catalog.NewContributor) bool {
		return c.Role == "" || c.Role == catalog.RoleAuthor
	})
}

func yearsKnown(a, b string) bool {
	_, oka := year(a)
	_, okb := year(b)
	return oka && okb
}
