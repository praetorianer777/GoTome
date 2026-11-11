package metadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ProfileFormat names the kind of file an export is.
const ProfileFormat = "gotome-match-weights"

// Profile is a learned score as it is stored, exported and shipped. It
// holds weights and counts and nothing of the books they came from: no
// title, author, ISBN, path or ID.
type Profile struct {
	Format         string `json:"format"`
	FeatureVersion int    `json:"featureVersion"`
	// GotomeVersion is the release that learned it.
	GotomeVersion string `json:"gotomeVersion"`
	// Books is how many books were learned from.
	Books int `json:"books"`
	// Languages are the shares of the sampled books by their language (de,
	// en, other) and Providers the shares of the candidates by provider.
	Languages map[string]float64 `json:"languages"`
	Providers map[string]float64 `json:"providers"`
	Weights   Weights            `json:"weights"`
}

var (
	// ErrProfileVersion is a file of weights of another feature set.
	ErrProfileVersion = errors.New("the weights are of another version of the features")
	// ErrProfileInvalid is a file that is not a file of weights.
	ErrProfileInvalid = errors.New("this is not a usable file of weights")
)

var providerName = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// knownFeatures are the names a file of weights may carry besides those of
// providers.
var knownFeatures = map[string]bool{
	FeatureBias: true, FeatureTitle: true, FeatureTitleExact: true, FeatureAuthors: true, FeatureAuthorsSilent: true,
	FeatureYearSame: true, FeatureYearNear: true, FeatureYearSilent: true, FeatureLanguageSame: true,
	FeatureLanguageSilent: true, FeatureLanguageOther: true, FeaturePagesNear: true, FeaturePagesFar: true,
	FeaturePublisherSame: true, FeaturePublisherDiff: true, FeatureSeriesSame: true, FeatureSeriesDiff: true,
	FeaturePositionSame: true, FeaturePositionDiff: true, FeatureMediumSame: true, FeatureMediumDiff: true,
	FeatureFirstInList: true, FeatureLateInList: true, FeatureDescription: true,
}

// maxProfileBytes is the largest file of weights taken.
const maxProfileBytes = 64 << 10

// ParseProfile reads and checks a file of weights.
func ParseProfile(raw []byte) (Profile, error) {
	if len(raw) > maxProfileBytes {
		return Profile{}, fmt.Errorf("%w: it is too large", ErrProfileInvalid)
	}
	var p Profile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("%w: %v", ErrProfileInvalid, err)
	}
	return p, p.Check()
}

// Check says whether the profile is one this build can use.
func (p Profile) Check() error {
	if p.Format != ProfileFormat {
		return fmt.Errorf("%w: it is not a %s file", ErrProfileInvalid, ProfileFormat)
	}
	if p.FeatureVersion != FeatureVersion {
		return fmt.Errorf("%w: it is of version %d, this GOtome uses %d", ErrProfileVersion, p.FeatureVersion, FeatureVersion)
	}
	if err := p.Weights.Valid(); err != nil {
		return fmt.Errorf("%w: %v", ErrProfileInvalid, err)
	}
	for name := range p.Weights {
		if provider, ok := strings.CutPrefix(name, featureProviderPrefix); ok && providerName.MatchString(provider) {
			continue
		}
		if !knownFeatures[name] {
			return fmt.Errorf("%w: it weighs %q, which is no feature", ErrProfileInvalid, name)
		}
	}
	for _, shares := range []map[string]float64{p.Languages, p.Providers} {
		if len(shares) > 20 {
			return fmt.Errorf("%w: too many shares", ErrProfileInvalid)
		}
		for name, v := range shares {
			if !providerName.MatchString(name) || math.IsNaN(v) || v < 0 || v > 1 {
				return fmt.Errorf("%w: a share is not usable", ErrProfileInvalid)
			}
		}
	}
	if p.Books < 0 || len(p.GotomeVersion) > 40 {
		return fmt.Errorf("%w: the counts are not usable", ErrProfileInvalid)
	}
	return nil
}

// shipped are the starting weights that come with a release, by the kind
// of library they were learned on. They are built from files learned
// profiles were exported to, which the owner has checked.
type shipped struct {
	name    string
	profile Profile
	// fits says whether a library with these shares of languages (by the
	// books' language, the empty one for books that have none) is of the
	// kind.
	fits func(shares map[string]float64) bool
}

// profileFor is the shipped profile that fits a library's mix of
// languages, if any.
func profileFor(shares map[string]float64) (string, *Profile) {
	for _, s := range shippedProfiles {
		if s.fits(shares) {
			p := s.profile
			return s.name, &p
		}
	}
	return "", nil
}

// majority is the fit of a profile for libraries in which most books
// that name a language are in it.
func majority(language string) func(map[string]float64) bool {
	return func(shares map[string]float64) bool {
		named := 0.0
		for l, v := range shares {
			if l != "" {
				named += v
			}
		}
		return named > 0 && shares[language]/named > 0.5
	}
}
