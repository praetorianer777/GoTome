package updates

import "testing"

func TestNewerComparesReleasesAsNumbers(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.13.0", "0.12.0", true},
		{"v0.13.0", "0.12.9", true},
		{"0.12.0", "0.12.0", false},
		{"0.11.0", "0.12.0", false},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.14.0-rc1", "0.12.0", false},
		{"0.14.0", "dev", false},
		{"0.14.0", "", false},
		{"0.14", "0.12.0", false},
		{"0.014.0", "0.12.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestOnlyASecuritySectionMarksARelease(t *testing.T) {
	for notes, want := range map[string]bool{
		"## Added\n- x\n\n### Security\n- y\n": true,
		"### security  \n- y":                  true,
		"Mentions security in a sentence.":     false,
		"### Security fixes for later\n":       false,
	} {
		if got := securitySection.MatchString(notes); got != want {
			t.Errorf("%q: %v, want %v", notes, got, want)
		}
	}
}
