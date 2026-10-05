package dedupe

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// novel is n words of made-up prose, the same for the same seed.
func novel(seed uint64, n int) string {
	r := rand.New(rand.NewPCG(seed, 1))
	syllables := []string{"ka", "lo", "mi", "ren", "tu", "sa", "vel", "or", "ni", "da", "esh", "pa", "qui", "zor"}
	vocabulary := make([]string, 4000)
	for i := range vocabulary {
		var w strings.Builder
		for range 1 + i%3 {
			w.WriteString(syllables[(i*7+w.Len()*13)%len(syllables)])
		}
		vocabulary[i] = fmt.Sprintf("%s%d", w.String(), i)
	}
	out := make([]string, n)
	for i := range out {
		out[i] = vocabulary[r.IntN(len(vocabulary))]
	}
	return strings.Join(out, " ")
}

// asPDF is the text as a PDF's pages give it: a running head and a page
// number every 300 words, and words broken across lines.
func asPDF(text string) string {
	ws := strings.Fields(text)
	var b strings.Builder
	for i, w := range ws {
		if i%300 == 0 {
			fmt.Fprintf(&b, "\nTHE BOOK %d\n", i/300+1)
		}
		if i%97 == 0 && len(w) > 4 {
			w = w[:3] + "-\n" + w[3:]
		}
		b.WriteString(w)
		b.WriteByte(' ')
	}
	return b.String()
}

func sharesBucket(a, b Signature, whole bool) bool {
	for _, x := range a.Buckets {
		if whole && x.Band >= bands {
			continue
		}
		if slices.Contains(b.Buckets, x) {
			return true
		}
	}
	return false
}

func mustSign(t *testing.T, texts ...string) Signature {
	t.Helper()
	sig, ok := Sign(texts)
	if !ok {
		t.Fatal("not signed")
	}
	return sig
}

func TestSignaturesTellTheSameTextFromAnother(t *testing.T) {
	text := novel(1, 60000)
	a := mustSign(t, text)
	if b := mustSign(t, text[:len(text)/2], text[len(text)/2:]); Jaccard(a.Values, b.Values) != 1 {
		t.Errorf("the same text in two chunks differs")
	}
	pdf := mustSign(t, asPDF(text))
	if j := Jaccard(a.Values, pdf.Values); j < 0.6 || !sharesBucket(a, pdf, true) {
		t.Errorf("as a PDF: jaccard %.2f, shares a bucket %v", j, sharesBucket(a, pdf, true))
	}
	other := mustSign(t, novel(2, 60000))
	if j := Jaccard(a.Values, other.Values); j > 0.05 || sharesBucket(a, other, false) {
		t.Errorf("another text: jaccard %.2f, shares a bucket %v", j, sharesBucket(a, other, false))
	}
}

func TestANovelIsFoundInTheOmnibusThatHoldsIt(t *testing.T) {
	var parts []string
	for i := range 5 {
		parts = append(parts, novel(uint64(10+i), 100000))
	}
	omnibus := mustSign(t, strings.Join(parts, " "))
	third := mustSign(t, parts[2])
	j := Jaccard(third.Values, omnibus.Values)
	in, holds := Containment(j, third.Shingles, omnibus.Shingles)
	if in < 0.8 || holds > 0.3 {
		t.Errorf("jaccard %.2f: novel in omnibus %.2f, omnibus in novel %.2f", j, in, holds)
	}
	// Too little alike for the whole-text buckets; the segments find it.
	if !sharesBucket(third, omnibus, false) {
		t.Error("the novel and the omnibus share no bucket")
	}
}

func TestShortTextsAreNotSigned(t *testing.T) {
	if _, ok := Sign([]string{novel(3, 200)}); ok {
		t.Error("a short text was signed")
	}
}

func TestContainmentOfEqualTexts(t *testing.T) {
	if a, b := Containment(1, 1000, 1000); a != 1 || b != 1 {
		t.Errorf("%v %v", a, b)
	}
	if got := decode(encode([]uint64{1, 1 << 63})); !slices.Equal(got, []uint64{1, 1 << 63}) {
		t.Errorf("round trip: %v", got)
	}
}
