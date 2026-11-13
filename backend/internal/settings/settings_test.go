package settings

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestASettingOfChoicesTakesOnlyThem(t *testing.T) {
	for _, c := range []struct {
		key, value, want string
		refused          bool
	}{
		{key: GenreSuggestions, value: " Ensemble ", want: "ensemble"},
		{key: GenreSuggestions, value: "combined", refused: true},
		{key: GenreSuggestions, value: "everything", refused: true},
		{key: AutoMatch, value: "OFF", want: "off"},
		{key: AutoMatch, value: "yes", refused: true},
		{key: OIDCDefaultRole, value: "none", want: "none"},
		{key: Providers, value: "Hardcover, openlibrary,hardcover", want: "hardcover,openlibrary"},
		{key: Providers, value: "openlibrary,nonesuch", refused: true},
		{key: Providers, value: " , ", refused: true},
		{key: EmbeddingModel, value: "nonesuch", refused: true},
	} {
		d, _ := definition(c.key)
		got, err := d.clean(c.value)
		if c.refused != (err != nil) || got != c.want {
			t.Errorf("%s %q: %q %v", c.key, c.value, got, err)
		}
	}
}

func TestEveryDefaultIsOneOfTheChoices(t *testing.T) {
	for _, d := range Definitions {
		if d.Choices == nil {
			continue
		}
		if got, err := d.clean(d.Default); err != nil || got != d.Default {
			t.Errorf("%s: the default %q is %q %v", d.Key, d.Default, got, err)
		}
		if d.Multiple && slices.Contains(d.Choices, "") {
			t.Errorf("%s: an empty choice", d.Key)
		}
	}
}

// The settings page names each choice in both languages; on and off are a
// switch, and the models are listed with their facts.
func TestEveryChoiceHasItsText(t *testing.T) {
	for _, file := range []string{"en.ts", "de.ts"} {
		b, err := os.ReadFile("../../../web/src/i18n/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range Definitions {
			if d.Choices == nil || slices.Equal(d.Choices, OnOff) || d.Key == EmbeddingModel {
				continue
			}
			for _, c := range d.Choices {
				if key := `"settingChoice.` + d.Key + "." + c + `"`; !strings.Contains(string(b), key) {
					t.Errorf("%s lacks %s", file, key)
				}
			}
		}
	}
}

func TestAThresholdIsANumberFromHalfToOne(t *testing.T) {
	for _, c := range []struct {
		value, want string
		refused     bool
	}{
		{value: "0.95", want: "0.95"},
		{value: " 0.5 ", want: "0.5"},
		{value: "1", want: "1"},
		{value: "0.90", want: "0.9"},
		{value: "0.49", refused: true},
		{value: "1.01", refused: true},
		{value: "high", refused: true},
		{value: "", refused: true},
	} {
		got, err := threshold(c.value)
		if c.refused != (err != nil) || got != c.want {
			t.Errorf("%q: %q %v", c.value, got, err)
		}
	}
}
