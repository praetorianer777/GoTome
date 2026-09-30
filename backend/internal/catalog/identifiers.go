package catalog

import (
	"strings"
	"time"
)

// Identifier types, as stored.
const (
	IDISBN        = "isbn"
	IDASIN        = "asin"
	IDDOI         = "doi"
	IDUUID        = "uuid"
	IDOpenLibrary = "openlibrary"
	IDGoogle      = "google"
	IDHardcover   = "hardcover"
	IDOther       = "other"
)

var identifierTypes = map[string]bool{
	IDISBN: true, IDASIN: true, IDDOI: true, IDUUID: true,
	IDOpenLibrary: true, IDGoogle: true, IDHardcover: true, IDOther: true,
}

// Identifier is one of a book's identifiers, in the form it is compared in.
type Identifier struct {
	Type  string
	Value string
}

// NormalizeIdentifier brings an identifier from a file or a provider into the
// form it is stored and compared in. ok is false for a value that is empty or,
// claiming to be an ISBN, is not one: a mistyped ISBN that matched nothing
// would be worse than none.
func NormalizeIdentifier(scheme, value string) (id Identifier, ok bool) {
	value = strings.TrimSpace(value)
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if value == "" {
		return Identifier{}, false
	}
	switch scheme {
	case "isbn", "isbn10", "isbn13", "isbn-10", "isbn-13":
		isbn, valid := NormalizeISBN(value)
		return Identifier{Type: IDISBN, Value: isbn}, valid
	case "doi":
		value = strings.ToLower(value)
		for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
			value = strings.TrimPrefix(value, prefix)
		}
		return Identifier{Type: IDDOI, Value: value}, strings.HasPrefix(value, "10.")
	case "asin", "mobi-asin", "amazon":
		return Identifier{Type: IDASIN, Value: strings.ToUpper(value)}, true
	case "uuid":
		return Identifier{Type: IDUUID, Value: strings.ToLower(strings.TrimPrefix(strings.ToLower(value), "urn:uuid:"))}, true
	case "":
		// Files often carry an ISBN without saying that it is one.
		if isbn, valid := NormalizeISBN(value); valid {
			return Identifier{Type: IDISBN, Value: isbn}, true
		}
		return Identifier{Type: IDOther, Value: value}, true
	}
	if identifierTypes[scheme] {
		return Identifier{Type: scheme, Value: value}, true
	}
	return Identifier{Type: IDOther, Value: scheme + ":" + value}, true
}

// NormalizeISBN returns the ISBN as 13 digits. It accepts ISBN-10 and ISBN-13
// with or without hyphens and spaces, and refuses one whose check digit is
// wrong.
func NormalizeISBN(s string) (string, bool) {
	var digits []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = append(digits, c)
		case c == 'X' || c == 'x':
			digits = append(digits, 'X')
		case c == '-' || c == ' ':
		default:
			return "", false
		}
	}
	switch len(digits) {
	case 10:
		sum := 0
		for i, c := range digits {
			v := int(c - '0')
			if c == 'X' {
				// X stands for ten, and only as the check digit.
				if i != 9 {
					return "", false
				}
				v = 10
			}
			sum += v * (10 - i)
		}
		if sum%11 != 0 {
			return "", false
		}
		body := "978" + string(digits[:9])
		return body + string(rune('0'+isbn13Check(body))), true
	case 13:
		for _, c := range digits {
			if c == 'X' {
				return "", false
			}
		}
		if isbn13Check(string(digits[:12])) != int(digits[12]-'0') {
			return "", false
		}
		return string(digits), true
	}
	return "", false
}

func isbn13Check(first12 string) int {
	sum := 0
	for i, c := range first12 {
		v := int(c - '0')
		if i%2 == 1 {
			v *= 3
		}
		sum += v
	}
	return (10 - sum%10) % 10
}

// Date precisions, as stored.
const (
	PrecisionYear  = "year"
	PrecisionMonth = "month"
	PrecisionDay   = "day"
)

// ParsePublished reads a publication date as files and providers write it:
// "2010", "2010-08", "2010-08-31", or a full timestamp. It returns how much of
// the date was actually given, so a bare year is not later shown as the first
// of January.
func ParsePublished(s string) (date time.Time, precision string, ok bool) {
	s = strings.TrimSpace(s)
	layouts := []struct{ layout, precision string }{
		{time.RFC3339, PrecisionDay},
		{"2006-01-02T15:04:05", PrecisionDay},
		{"2006-01-02", PrecisionDay},
		{"2006-01", PrecisionMonth},
		{"2006", PrecisionYear},
	}
	for _, l := range layouts {
		t, err := time.Parse(l.layout, s)
		if err != nil {
			continue
		}
		// Year zero and the far future are placeholders, not dates.
		if t.Year() < 1 || t.Year() > 9999 {
			return time.Time{}, "", false
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), l.precision, true
	}
	return time.Time{}, "", false
}
