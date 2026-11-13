// Package organise works out where the files of a library belong by their
// books' metadata: a pattern, the safe spelling of its parts, and the names
// that come of them. It touches no disk and no database.
package organise

import (
	"fmt"
	"sort"
	"strings"
)

// DefaultPattern lays a library out by author and series. The square
// brackets hold what is left out when a placeholder in them is empty, so a
// book without a series lies at {author}/{title}.{ext}.
const DefaultPattern = "{author}/[{series}/][{series_index} - ]{title}.{ext}"

// Placeholders are the names a pattern may use.
var Placeholders = []string{
	"author", "author_sort", "title", "series", "series_index", "year", "language", "ext",
}

// MaxPatternLength is the longest pattern accepted, in bytes.
const MaxPatternLength = 300

// part is a piece of a pattern: text, a placeholder, or a group that is
// left out when a placeholder in it is empty.
type part struct {
	text  string
	name  string
	group []part
}

// Pattern is a parsed pattern.
type Pattern struct {
	source string
	parts  []part
}

// String is the pattern as written.
func (p Pattern) String() string { return p.source }

// Parse reads a pattern. It must end in {ext}, must not start or end with a
// slash or hold an empty folder, and may use only the known placeholders.
func Parse(src string) (Pattern, error) {
	if src == "" {
		src = DefaultPattern
	}
	if len(src) > MaxPatternLength {
		return Pattern{}, fmt.Errorf("the pattern is longer than %d characters", MaxPatternLength)
	}
	if strings.ContainsFunc(src, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return Pattern{}, fmt.Errorf("the pattern has a control character")
	}
	if strings.Contains(src, "\\") || strings.Contains(src, "//") || strings.HasPrefix(src, "/") || strings.HasSuffix(src, "/") {
		return Pattern{}, fmt.Errorf("write folders as name/name, with no backslash, empty folder or slash at either end")
	}
	parts, rest, err := parseParts(src, false)
	if err != nil {
		return Pattern{}, err
	}
	if rest != "" {
		return Pattern{}, fmt.Errorf("a closing bracket has no opening one")
	}
	if !strings.HasSuffix(src, "{ext}") {
		return Pattern{}, fmt.Errorf("the pattern must end in {ext}")
	}
	if !usesOutside(parts, "title") {
		return Pattern{}, fmt.Errorf("the pattern must use {title} outside square brackets, or two books could share a name")
	}
	return Pattern{source: src, parts: parts}, nil
}

func usesOutside(parts []part, name string) bool {
	for _, p := range parts {
		if p.name == name {
			return true
		}
	}
	return false
}

func parseParts(s string, inGroup bool) ([]part, string, error) {
	var out []part
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, part{text: text.String()})
			text.Reset()
		}
	}
	for len(s) > 0 {
		switch s[0] {
		case '{':
			end := strings.IndexByte(s, '}')
			if end < 0 {
				return nil, "", fmt.Errorf("a { has no closing }")
			}
			name := s[1:end]
			if !known(name) {
				return nil, "", fmt.Errorf("{%s} is not a placeholder; use %s", name, listPlaceholders())
			}
			flush()
			out = append(out, part{name: name})
			s = s[end+1:]
		case '[':
			if inGroup {
				return nil, "", fmt.Errorf("square brackets cannot be nested")
			}
			flush()
			group, rest, err := parseParts(s[1:], true)
			if err != nil {
				return nil, "", err
			}
			out = append(out, part{group: group})
			s = rest[1:]
		case ']':
			if !inGroup {
				return nil, "", fmt.Errorf("a closing bracket has no opening one")
			}
			flush()
			return out, s, nil
		case '}':
			return nil, "", fmt.Errorf("a } has no opening {")
		default:
			text.WriteByte(s[0])
			s = s[1:]
		}
	}
	if inGroup {
		return nil, "", fmt.Errorf("a [ has no closing ]")
	}
	flush()
	return out, "", nil
}

func known(name string) bool {
	for _, p := range Placeholders {
		if p == name {
			return true
		}
	}
	return false
}

func listPlaceholders() string {
	names := append([]string(nil), Placeholders...)
	sort.Strings(names)
	for i, n := range names {
		names[i] = "{" + n + "}"
	}
	return strings.Join(names, ", ")
}

// Values are what the placeholders stand for, as plain text. A value that
// is empty is not known.
type Values map[string]string

func render(parts []part, v Values, strict bool) (string, bool) {
	var b strings.Builder
	for _, p := range parts {
		switch {
		case p.name != "":
			value := v[p.name]
			if value == "" && strict {
				return "", false
			}
			b.WriteString(Clean(value))
		case p.group != nil:
			if s, ok := render(p.group, v, true); ok {
				b.WriteString(s)
			}
		default:
			b.WriteString(p.text)
		}
	}
	return b.String(), true
}

// PatternOr is the pattern a library stores, or the default when it stores
// none.
func PatternOr(stored *string) string {
	if stored == nil || *stored == "" {
		return DefaultPattern
	}
	return *stored
}
