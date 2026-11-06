package cleanup

import "testing"

// seriesTitles are titles with a series in them and what is read from
// them.
var seriesTitles = map[string]SeriesTitle{
	"Rain Wild Chronicles 01 - Drachenhüter":        {"Rain Wild Chronicles", 1, "Drachenhüter"},
	"Rain Wild Chronicles 2 – Drachenkämpfer":       {"Rain Wild Chronicles", 2, "Drachenkämpfer"},
	"Wheel of Time #2: The Great Hunt":              {"Wheel of Time", 2, "The Great Hunt"},
	"Die Zwerge, Band 3: Das Schicksal der Zwerge":  {"Die Zwerge", 3, "Das Schicksal der Zwerge"},
	"Die Zwerge Bd. 4 - Die Rache der Zwerge":       {"Die Zwerge", 4, "Die Rache der Zwerge"},
	"Perry Rhodan - 2500 - Projekt Saturn":          {"Perry Rhodan", 2500, "Projekt Saturn"},
	"Jerry Cotton 2913: Die beste Waffe":            {"Jerry Cotton", 2913, "Die beste Waffe"},
	"Star Trek - Deep Space Nine 12 - Der Weg":      {"Star Trek - Deep Space Nine", 12, "Der Weg"},
	"Die drei ??? Folge 125 - Feuermond":            {"Die drei ???", 125, "Feuermond"},
	"Der Herr der Ringe (1) - Die Gefährten":        {"Der Herr der Ringe", 1, "Die Gefährten"},
	"The Expanse Book 1.5: The Butcher of Anderson": {"The Expanse", 1.5, "The Butcher of Anderson"},
	"Drachenhüter (Rain Wild Chronicles #1)":        {"Rain Wild Chronicles", 1, "Drachenhüter"},
	"Die Gefährten (Der Herr der Ringe, Band 1)":    {"Der Herr der Ringe", 1, "Die Gefährten"},
	"Ödland (Die Ödland-Chroniken Teil 2)":          {"Die Ödland-Chroniken", 2, "Ödland"},
	"Mord (in Paris) (Kommissar Dupin Nr. 3)":       {"Kommissar Dupin", 3, "Mord (in Paris)"},
	"Es - Teil 1 - Der Club der Verlierer":          {"Es", 1, "Der Club der Verlierer"},
	"Fahrenheit 451 - Ein Roman":                    {"Fahrenheit", 451, "Ein Roman"},
}

// noSeriesTitles are titles that hold no series.
var noSeriesTitles = []string{
	"1984 - Roman",
	"2001 - Odyssee im Weltraum",
	"Berlin 1945 - Das Ende",
	"Catch22 - Der Roman",
	"Krieg und Frieden - Teil 1",
	"Krieg und Frieden, Teil 1",
	"Teil 2 - Die Rückkehr",
	"Band 3: Das Ende",
	"Der Hobbit (Teil 1)",
	"Drachenhüter (Band 1)",
	"Die 39 Stufen",
	"Rain Wild Chronicles 01 Drachenhüter",
	"Das letzte Evangelium: Historischer Roman (German Edition)",
	"Sterbewohl: Kriminalroman",
	"Die Legenden der Albae: Die Vergessenen",
	"Ocean's 11 - ",
	"A 1 - 2",
	"",
}

func TestSeriesInTitle(t *testing.T) {
	for title, want := range seriesTitles {
		got, ok := SeriesInTitle(title)
		if !ok || got != want {
			t.Errorf("SeriesInTitle(%q) = %+v %v, want %+v", title, got, ok, want)
		}
	}
	for _, title := range noSeriesTitles {
		if got, ok := SeriesInTitle(title); ok {
			t.Errorf("SeriesInTitle(%q) = %+v, want none", title, got)
		}
	}
}
