package fontsubset

import (
	"fmt"
	"slices"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

const verifyPpem = fixed.Int26_6(64 * 100)

// Verify parses both fonts and checks that every rune the original maps to
// a glyph is mapped by the subset to a glyph with the same outline and
// advance.
func Verify(original, subset []byte, runes []rune) error {
	a, err := parse(original)
	if err != nil {
		return fmt.Errorf("original: %w", err)
	}
	b, err := parse(subset)
	if err != nil {
		return fmt.Errorf("subset: %w", err)
	}
	var buf sfnt.Buffer
	for _, r := range runes {
		ga, err := a.GlyphIndex(&buf, r)
		if err != nil || ga == 0 {
			continue
		}
		gb, err := b.GlyphIndex(&buf, r)
		if err != nil || gb == 0 {
			return fmt.Errorf("%q has no glyph in the subset", r)
		}
		sa, err := a.LoadGlyph(&buf, ga, verifyPpem, nil)
		if err != nil {
			continue
		}
		sa = slices.Clone(sa)
		sb, err := b.LoadGlyph(&buf, gb, verifyPpem, nil)
		if err != nil || !slices.Equal(sa, sb) {
			return fmt.Errorf("%q has another outline in the subset", r)
		}
		adva, errA := a.GlyphAdvance(&buf, ga, verifyPpem, 0)
		advb, errB := b.GlyphAdvance(&buf, gb, verifyPpem, 0)
		if errA == nil && (errB != nil || adva != advb) {
			return fmt.Errorf("%q has another advance in the subset", r)
		}
	}
	return nil
}

func parse(font []byte) (*sfnt.Font, error) {
	plain, err := ToSFNT(font)
	if err != nil {
		return nil, err
	}
	return sfnt.Parse(plain)
}
