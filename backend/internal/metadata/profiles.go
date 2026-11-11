package metadata

// shippedProfiles are tried in order; the first that fits a library is
// its starting score until it has learned its own.
var shippedProfiles = []shipped{
	// german: learned by match-train on a library in German, 1,000 books
	// looked up at the DNB and Open Library. Numbers only.
	{
		name: "german",
		fits: majority("de"),
		profile: Profile{
			Format: ProfileFormat, FeatureVersion: FeatureVersion, GotomeVersion: "0.18.0", Books: 1000,
			Languages: map[string]float64{"de": 0.99, "en": 0.01, "other": 0},
			Providers: map[string]float64{"dnb": 0.87, "openlibrary": 0.13},
			Weights: Weights{
				"authors":              -0.42,
				"authorsSilent":        -0.959,
				"bias":                 -4.984,
				"firstInList":          -0.133,
				"languageOther":        -0.265,
				"languageSame":         0.079,
				"languageSilent":       0.186,
				"lateInList":           -0.479,
				"mediumDiff":           0.407,
				"mediumSame":           1.598,
				"pagesFar":             -0.348,
				"pagesNear":            0.601,
				"positionDiff":         0.647,
				"positionSame":         -0.763,
				"provider:dnb":         1.894,
				"provider:openlibrary": -1.894,
				"publisherDiff":        -0.736,
				"publisherSame":        0.458,
				"seriesDiff":           0.756,
				"seriesSame":           0.232,
				"title":                1.051,
				"titleExact":           -0.125,
				"yearNear":             0.731,
				"yearSame":             2.097,
				"yearSilent":           0.998,
			},
		},
	},
}
