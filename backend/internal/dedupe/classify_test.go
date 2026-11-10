package dedupe

import "testing"

func overlap(j, ab, ba string) Found {
	return Found{Kind: EvidenceOverlap, Detail: "jaccard=" + j + " a_in_b=" + ab + " b_in_a=" + ba}
}

func TestClassify(t *testing.T) {
	titleAuthor := Found{Kind: EvidenceTitleAuthor, Detail: "der sternwanderer / lena brandt"}
	isbn := Found{Kind: EvidenceISBN, Detail: "9783161484100"}
	for _, c := range []struct {
		name     string
		evidence []Found
		class    string
		holder   string
	}{
		{"same bytes", []Found{{Kind: EvidenceSHA256, Detail: "ab"}}, ClassSameText, ""},
		{"same content", []Found{{Kind: EvidenceContent, Detail: "ab"}}, ClassSameText, ""},
		{"same bytes beat a low overlap", []Found{overlap("0.30", "0.90", "0.30"), {Kind: EvidenceSHA256, Detail: "ab"}}, ClassSameText, ""},
		{"overlap at the high edge", []Found{overlap("0.95", "0.97", "0.98")}, ClassOverlapHigh, ""},
		{"overlap just under the high edge", []Found{overlap("0.94", "0.97", "0.98")}, ClassOverlapMid, ""},
		{"overlap at the measured edge", []Found{overlap("0.80", "0.9", "0.9")}, ClassOverlapMid, ""},
		{"overlap just under the measured edge", []Found{overlap("0.79", "0.9", "0.9")}, ClassPerson, ""},
		{"overlap in the middle", []Found{overlap("0.65", "0.8", "0.8")}, ClassPerson, ""},
		{"high overlap with the same title and author", []Found{overlap("0.97", "0.98", "0.98"), titleAuthor}, ClassOverlapHigh, ""},
		{"one inside the other, a first", []Found{overlap("0.20", "0.97", "0.12")}, ClassContained, "b"},
		{"one inside the other, b first", []Found{overlap("0.20", "0.10", "0.99")}, ClassContained, "a"},
		{"containment at the edge", []Found{overlap("0.49", "0.95", "0.10")}, ClassContained, "b"},
		{"part inside", []Found{overlap("0.30", "0.85", "0.10")}, ClassPartInside, "b"},
		{"part inside at the edge", []Found{overlap("0.30", "0.80", "0.10")}, ClassPartInside, "b"},
		{"held too little", []Found{overlap("0.30", "0.79", "0.10")}, ClassPerson, ""},
		{"both hold the same share", []Found{overlap("0.30", "0.96", "0.96")}, ClassPerson, ""},
		{"contained but an ISBN is shared", []Found{overlap("0.20", "0.97", "0.12"), isbn}, ClassPerson, ""},
		{"an ISBN alone", []Found{isbn}, ClassPerson, ""},
		{"title and author alone", []Found{titleAuthor}, ClassPerson, ""},
		{"no evidence", nil, ClassPerson, ""},
		{"unreadable overlap", []Found{{Kind: EvidenceOverlap, Detail: "much"}}, ClassPerson, ""},
		{"the strongest of two overlaps", []Found{overlap("0.10", "0.2", "0.2"), overlap("0.96", "0.97", "0.97")}, ClassOverlapHigh, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Classify(c.evidence)
			if got.Class != c.class || got.Holder != c.holder {
				t.Errorf("Classify = %s holder %q, want %s holder %q", got.Class, got.Holder, c.class, c.holder)
			}
		})
	}
}

func TestEveryAutoClassHasAnAction(t *testing.T) {
	for _, c := range AutoClasses {
		if Merges(c) == Links(c) {
			t.Errorf("%s must either merge or link", c)
		}
	}
}
