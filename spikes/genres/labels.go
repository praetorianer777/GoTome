package main

import (
	"regexp"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/genres"
)

// lcshRules map Library of Congress subject headings, which Project
// Gutenberg's EPUBs carry, to GOtome's genres where genres.Spellings has no
// spelling. A rule matches when the lower-cased heading contains the text;
// fiction says the book must also have a heading with "-- fiction" or a
// fiction form ("stories", "novels"), so that a history of a war is not a
// war novel.
type lcshRule struct {
	contains string
	genre    string
	fiction  bool
}

var lcshRules = []lcshRule{
	{"western stories", "western", false}, {"cowboys", "western", false}, {"cattle stealing", "western", false},
	{"sheriffs", "western", false}, {"ranch life", "western", false},
	{"world war, 1914-1918", "war", true}, {"world war, 1939-1945", "war", true}, {"napoleonic wars", "war", true},
	{"war stories", "war", false}, {"soldiers", "war", true}, {"world war, 1914-1918", "history", false},
	{"world war, 1939-1945", "history", false},
	{"man-woman relationships", "romance", true}, {"mate selection", "romance", true}, {"love stories", "romance", false},
	{"courtship", "romance", true}, {"romance fiction", "romance", false},
	{"mystery fiction", "crime", false}, {"detective and mystery", "crime", false}, {"murder", "crime", true},
	{"adventure stories", "adventure", false}, {"sea stories", "adventure", false}, {"seafaring life", "adventure", true},
	{"fantasy fiction", "fantasy", false}, {"fairy tales", "myths", false}, {"folklore", "myths", false},
	{"mythology", "myths", false}, {"legends", "myths", false},
	{"humorous stories", "humour", false}, {"wit and humor", "humour", false}, {"comedy plays", "humour", false},
	{"poetry", "poetry", false}, {"tragedies", "drama", false}, {"one-act plays", "drama", false}, {"comedy plays", "drama", false},
	{"drama", "drama", false}, {"operas", "arts", false},
	{"autobiograph", "biography", false}, {"biography", "biography", false}, {"diaries", "biography", false},
	{"christian life", "religion", false}, {"catholic church", "religion", false}, {"jesus christ", "religion", false},
	{"sunday school", "religion", false}, {"bible", "religion", false}, {"theology", "religion", false}, {"sermons", "religion", false},
	{"conduct of life", "self-help", false}, {"psychoanalysis", "psychology", false}, {"psychology", "psychology", false},
	{"socialism", "society", false}, {"socialists", "society", false}, {"revolutionaries", "society", false},
	{"political science", "society", false}, {"social classes", "society", false},
	{"natural history", "nature", false}, {"nature conservation", "nature", false}, {"forests and forestry", "nature", false},
	{"geology", "science", false}, {"astronomy", "science", false}, {"paleontology", "science", false}, {"physics", "science", false},
	{"chemistry", "science", false}, {"mathematics", "science", false}, {"biology", "science", false}, {"evolution", "science", false},
	{"music", "arts", false}, {`art\b`, "arts", false}, {"painting", "arts", false}, {"architecture", "arts", false},
	{"english language", "language", false}, {"german language", "language", false}, {"french language", "language", false},
	{"knitting", "hobbies", false}, {"gardening", "hobbies", false}, {"handicraft", "hobbies", false},
	{"voyages around the world", "travel", false}, {"description and travel", "travel", false},
	{"juvenile fiction", "children", false}, {"children's stories", "children", false}, {"fairy tales", "children", false},
	{"philosophy", "philosophy", false}, {"ethics", "philosophy", false}, {"cookery", "cooking", false},
	{"medicine", "health", false}, {"hygiene", "health", false}, {`law\b`, "law", false},
	{"economics", "business", false}, {"science fiction", "science-fiction", false}, {"utopias", "science-fiction", false},
	{"ghost stories", "horror", false}, {"horror tales", "horror", false}, {"spy stories", "thriller", false},
	{"short stories", "short-stories", false}, {"historical fiction", "historical", false},
}

var compiled = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(lcshRules))
	for i, r := range lcshRules {
		p := r.contains
		if !strings.HasSuffix(p, `\b`) {
			p = regexp.QuoteMeta(p)
		}
		out[i] = regexp.MustCompile(`(?:^|[^a-z])` + p)
	}
	return out
}()

var fictionForm = regexp.MustCompile(`-- fiction|stories|novels|fiction$`)

// LabelsOf are the genres a book's subject headings give: GOtome's own
// spellings first, then lcshRules, and "-- history -- fiction" as a
// historical novel.
func LabelsOf(subjects []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(g string) {
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	for _, g := range genres.FromTags(subjects) {
		add(g)
	}
	fiction := false
	for _, s := range subjects {
		if fictionForm.MatchString(strings.ToLower(s)) {
			fiction = true
		}
	}
	for _, s := range subjects {
		l := strings.ToLower(s)
		for i, r := range lcshRules {
			if compiled[i].MatchString(l) && (!r.fiction || fiction) {
				// History of a war is for non-fiction only.
				if r.genre == "history" && fiction {
					continue
				}
				add(r.genre)
			}
		}
		if strings.Contains(l, "-- history") && strings.Contains(l, "-- fiction") {
			add("historical")
		}
	}
	return out
}
