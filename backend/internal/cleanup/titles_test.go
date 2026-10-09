package cleanup

import "testing"

func TestCleanTitle(t *testing.T) {
	for title, want := range map[string]string{
		"Das letzte Evangelium: Historischer Roman (German Edition)": "Das letzte Evangelium",
		"Rauklands Sohn: Raukland Trilogie (German Edition)":         "Rauklands Sohn: Raukland Trilogie",
		"Der Schwarm - Roman":                             "Der Schwarm",
		"Die Abrechnung -Thriller":                        "Die Abrechnung",
		"Sterbewohl: Kriminalroman":                       "Sterbewohl",
		"Das Original - Psycho-Thriller":                  "Das Original",
		"Die Sonnenposition. Ein Roman":                   "Die Sonnenposition. Ein Roman",
		"Der Distelfink: Ein Roman":                       "Der Distelfink",
		"The Road: A Novel":                               "The Road",
		"Tod im Moor (Kindle Edition)":                    "Tod im Moor",
		"Eifel-Krimi":                                     "Eifel-Krimi",
		"Mordsspaß-Krimi":                                 "Mordsspaß-Krimi",
		"Roman":                                           "Roman",
		"Der Roman":                                       "Der Roman",
		"Thriller (German Edition)":                       "Thriller",
		": Roman":                                         ": Roman",
		"Die Legenden der Albae: Die Vergessenen":         "Die Legenden der Albae: Die Vergessenen",
		"Romane und Erzählungen":                          "Romane und Erzählungen",
		"Kurz und gut – Erzählungen":                      "Kurz und gut",
		"Sommerhaus, später: Erzählungen (Gesamtausgabe)": "Sommerhaus, später: Erzählungen (Gesamtausgabe)",
	} {
		if got := CleanTitle(title); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", title, got, want)
		}
	}
}
