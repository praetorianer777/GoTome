package cleanup

import "testing"

func TestSpellingOnly(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"Lena Brandt", "Brandt, Lena", true},
		{"lena brandt", "LENA BRANDT", true},
		{"Jürgen Weiß", "Jurgen Weiss", true},
		{"J.R.R. Fenwick", "JRR Fenwick", true},
		{"J. R. R. Fenwick", "J R R Fenwick", true},
		{"Fenwick, J. R. R.", "J.R.R. Fenwick", true},
		{"Lena Brandt", "Lena  Brandt", true},
		{"Anna Maria", "Maria Anna", false},
		{"Lena Brandt", "Brandt Lena", false},
		{"John Fenwick", "J. Fenwick", false},
		{"Lena Brandt", "Lena K. Brandt", false},
		{"Lena Brandt", "Lena Brandts", false},
		{"", "", false},
	} {
		if got := SpellingOnly(c.a, c.b); got != c.want {
			t.Errorf("SpellingOnly(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
