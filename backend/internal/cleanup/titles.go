package cleanup

import (
	"regexp"
	"strings"
)

// TitleAdditions finds, at the end of a title, what a shop added to it: the
// edition it sold ("(German Edition)") or the genre after a colon or a dash
// (": Roman", " - Historischer Roman", " - Thriller"). A genre joined to a
// word ("Eifel-Krimi") is the title's own. The SQL function
// title_has_addition holds the same pattern, for Postgres (~*): change both.
const TitleAdditions = `\s*(\((german|english|kindle|french|spanish|italian|deutsche|englische)\s+(edition|ausgabe)\)` +
	`|(:|\s[-–])\s*((ein|historischer|psycho|fantasy|science[- ]fiction)[- ]?)?` +
	`(roman|kriminalroman|krimi|thriller|psychothriller|liebesroman|fantasyroman|erzählung|erzählungen|novelle|novel|a novel))\s*$`

var titleAdditions = regexp.MustCompile(`(?i)` + TitleAdditions)

// CleanTitle is a title without the additions at its end, or the title as
// it is when nothing would be left.
func CleanTitle(title string) string {
	out := strings.TrimSpace(title)
	for {
		next := strings.TrimSpace(titleAdditions.ReplaceAllString(out, ""))
		if next == out || next == "" {
			return out
		}
		out = next
	}
}
