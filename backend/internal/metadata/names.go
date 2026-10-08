package metadata

import (
	"slices"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// NaturalName turns a name filed as "Cornwell, Bernard" into "Bernard
// Cornwell", which is how providers know people. A name with no comma, or
// with several, which may be a list of names, is left as it is.
func NaturalName(name string) string {
	name = strings.TrimSpace(name)
	last, first, ok := strings.Cut(name, ",")
	if !ok || strings.Contains(first, ",") || strings.TrimSpace(first) == "" {
		return name
	}
	return strings.TrimSpace(first) + " " + strings.TrimSpace(last)
}

// NameKeys are the keys a name is compared by, in both orders.
func NameKeys(name string) []string {
	keys := []string{catalog.Key(name)}
	if k := catalog.Key(NaturalName(name)); !slices.Contains(keys, k) {
		keys = append(keys, k)
	}
	return keys
}

// SameName reports whether two names are one person's or one series',
// whichever order either is written in.
func SameName(a, b string) bool {
	return catalog.Key(NaturalName(a)) == catalog.Key(NaturalName(b))
}
