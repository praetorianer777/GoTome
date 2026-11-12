package cleanup

import "testing"

func TestCleanTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Das stille Archiv: Historischer Roman (German Edition)": "Das stille Archiv",
		"Brennholds Sohn: Brennhold Trilogie (German Edition)":   "Brennholds Sohn: Brennhold Trilogie",
		"Der Wirbel - Roman":                              "Der Wirbel",
		"Die Abrechnung -Thriller":                        "Die Abrechnung",
		"Stillwasser: Kriminalroman":                      "Stillwasser",
		"Das Original - Psycho-Thriller":                  "Das Original",
		"Die Sonnenposition. Ein Roman":                   "Die Sonnenposition. Ein Roman",
		"Der Zaunkönig: Ein Roman":                        "Der Zaunkönig",
		"The Ferry: A Novel":                              "The Ferry",
		"Tod im Moor (Kindle Edition)":                    "Tod im Moor",
		"Eifel-Krimi":                                     "Eifel-Krimi",
		"Mordsspaß-Krimi":                                 "Mordsspaß-Krimi",
		"Roman":                                           "Roman",
		"Der Roman":                                       "Der Roman",
		"Thriller (German Edition)":                       "Thriller",
		": Roman":                                         ": Roman",
		"Die Legenden der Nebelvölker: Die Vergessenen":   "Die Legenden der Nebelvölker: Die Vergessenen",
		"Romane und Erzählungen":                          "Romane und Erzählungen",
		"Kurz und gut – Erzählungen":                      "Kurz und gut",
		"Sommerhaus, später: Erzählungen (Gesamtausgabe)": "Sommerhaus, später: Erzählungen (Gesamtausgabe)",
	} {
		if got := CleanTitle(title); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", title, got, want)
		}
	}
}
