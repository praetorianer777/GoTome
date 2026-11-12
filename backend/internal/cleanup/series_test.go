package cleanup

import "testing"

// seriesTitles are titles with a series in them and what is read from
// them.
var seriesTitles = map[string]SeriesTitle{
	"Sternwanderer 01 - Leuchtturmwärter":                  {"Sternwanderer", 1, "Leuchtturmwärter"},
	"Sternwanderer 2 – Wellenbrecher":                      {"Sternwanderer", 2, "Wellenbrecher"},
	"Salt Road #2: The Long Crossing":                      {"Salt Road", 2, "The Long Crossing"},
	"Die Glasbläser, Band 3: Das Schicksal der Glasbläser": {"Die Glasbläser", 3, "Das Schicksal der Glasbläser"},
	"Die Glasbläser Bd. 4 - Die Rache der Glasbläser":      {"Die Glasbläser", 4, "Die Rache der Glasbläser"},
	"Nordlicht-Saga - 2500 - Projekt Kupfer":               {"Nordlicht-Saga", 2500, "Projekt Kupfer"},
	"Max Falk 2913: Die beste Waffe":                       {"Max Falk", 2913, "Die beste Waffe"},
	"Raumhafen - Station Vier 12 - Der Weg":                {"Raumhafen - Station Vier", 12, "Der Weg"},
	"Die Spürnasen ??? Folge 125 - Nachtflug":              {"Die Spürnasen ???", 125, "Nachtflug"},
	"Das Salzland (1) - Die Brückenbauer":                  {"Das Salzland", 1, "Die Brückenbauer"},
	"The Meridian Book 1.5: The Miller of Aldwyn":          {"The Meridian", 1.5, "The Miller of Aldwyn"},
	"Leuchtturmwärter (Sternwanderer #1)":                  {"Sternwanderer", 1, "Leuchtturmwärter"},
	"Die Brückenbauer (Das Salzland, Band 1)":              {"Das Salzland", 1, "Die Brückenbauer"},
	"Ödland (Die Ödland-Chroniken Teil 2)":                 {"Die Ödland-Chroniken", 2, "Ödland"},
	"Mord (in Paris) (Kommissar Lenz Nr. 3)":               {"Kommissar Lenz", 3, "Mord (in Paris)"},
	"Wo - Teil 1 - Der Club der Wanderer":                  {"Wo", 1, "Der Club der Wanderer"},
	"Celsius 212 - Ein Roman":                              {"Celsius", 212, "Ein Roman"},
}

// noSeriesTitles are titles that hold no series.
var noSeriesTitles = []string{
	"1957 - Roman",
	"2077 - Reise zum Mond",
	"Hamburg 1923 - Das Ende",
	"Trap17 - Der Roman",
	"Stadt und Land - Teil 1",
	"Stadt und Land, Teil 1",
	"Teil 2 - Die Rückkehr",
	"Band 3: Das Ende",
	"Die Laterne (Teil 1)",
	"Leuchtturmwärter (Band 1)",
	"Die 41 Stufen",
	"Sternwanderer 01 Leuchtturmwärter",
	"Das stille Archiv: Historischer Roman (German Edition)",
	"Stillwasser: Kriminalroman",
	"Die Legenden der Nebelvölker: Die Vergessenen",
	"Marlow's 7 - ",
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
