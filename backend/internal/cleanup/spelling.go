package cleanup

import (
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
	"github.com/praetorianer777/gotome/backend/internal/metadata"
)

// SpellingOnly reports whether two names are one person's written two ways
// and nothing else differs: "Last, First" against "First Last", case,
// accents, punctuation, and initials with or without points ("J.R.R." and
// "JRR"). Other word orders are not: "Anna Maria" and "Maria Anna" are two
// people to a person, though they have the same words. A full name against
// an initial is not either.
func SpellingOnly(a, b string) bool {
	ka, kb := spellingKey(a), spellingKey(b)
	return ka != "" && ka == kb
}

func spellingKey(name string) string {
	words := strings.Fields(catalog.Key(metadata.NaturalName(strings.Join(strings.Fields(name), " "))))
	var out []string
	initials := ""
	flush := func() {
		if initials != "" {
			out = append(out, initials)
			initials = ""
		}
	}
	for _, w := range words {
		if len([]rune(w)) == 1 {
			initials += w
			continue
		}
		flush()
		out = append(out, w)
	}
	flush()
	return strings.Join(out, " ")
}
