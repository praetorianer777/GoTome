package genres

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/praetorianer777/gotome/backend/internal/catalog"
)

// Spellings are the tags that mean each genre, as files and providers
// write them: German and English, trade headings of the Warengruppen-
// Systematik and of BISAC as single parts. They are compared by
// catalog.Key, so case, accents and punctuation do not matter. A tag of
// several parts ("Belletristik/Kriminalromane", "Fiction / Fantasy /
// General") is also read part by part (FromTag). Extend it freely; a tag
// that is in none stays a tag and gives no genre.
var Spellings = map[string][]string{
	"contemporary": {
		"Gegenwartsliteratur", "Gegenwartsliteratur (ab 1945)", "Belletristik/Gegenwartsliteratur (ab 1945)",
		"Zeitgenössische Literatur", "Moderne Literatur", "Literary fiction", "Literary", "Contemporary fiction",
		"Contemporary", "Contemporary literature", "Fiction / Literary", "Literatur", "Romane und Erzählungen der Gegenwart",
		"Women's fiction", "Frauenroman", "Frauenromane", "Coming of age", "Zeitgenössisch",
	},
	"classics": {
		"Klassiker", "Klassische Literatur", "Hauptwerk vor 1945", "Belletristik/Hauptwerk vor 1945", "Classics",
		"Classic", "Classic literature", "Classic fiction", "Weltliteratur", "Literary classics",
	},
	"crime": {
		"Krimi", "Krimis", "Kriminalroman", "Kriminalromane", "Kriminalliteratur", "Krimi-Thriller", "Regionalkrimi",
		"Regionalkrimis", "Kriminalromane und Mystery", "Krimis, Thriller, Spannung", "Krimi & Thriller", "Krimis & Thriller",
		"Belletristik/Kriminalromane", "Detektivroman", "Detective", "Detective fiction", "Detective and mystery stories",
		"Mystery", "Mysteries", "Mystery & detective", "Mystery and detective", "Crime", "Crime fiction", "Cozy mystery",
		"Police procedural", "Whodunit", "Krimikomödie", "Skandinavien-Krimi", "Schwedenkrimi", "Ostfrieslandkrimi",
		"Mord", "Historischer Krimi", "Historische Krimis", "Historische Kriminalromane", "Historical mystery",
	},
	"thriller": {
		"Thriller", "Thrillers", "Psychothriller", "Psychothriller & Spannung", "Spannung", "Spannungsroman",
		"Suspense", "Thrillers & suspense", "Thriller & Suspense", "Psychological thriller", "Politthriller",
		"Wissenschaftsthriller", "Medizinthriller", "Agententhriller", "Spionage", "Espionage", "Spy stories",
		"Legal thriller", "Justizthriller", "Action", "Action & adventure thriller", "Krimi-Thriller",
		"Krimis, Thriller, Spannung", "Krimi & Thriller", "Krimis & Thriller",
	},
	"horror": {
		"Horror", "Horrorroman", "Grusel", "Gruselgeschichten", "Schauerroman", "Horror fiction", "Ghost stories",
		"Geistergeschichten", "Vampire", "Vampires", "Paranormal", "Übernatürliches",
		"Vampir Horror Roman", "Dorian Hunter", "Dämonenkiller",
	},
	"science-fiction": {
		"Science Fiction", "Science-Fiction", "Sci-Fi", "SciFi", "SF", "Utopie", "Space opera", "Cyberpunk",
		"Military science fiction", "Weltraum", "Zukunftsroman", "Hard science fiction", "Zeitreise", "Time travel",
		"Aliens", "Außerirdische", "Science Fiction & Fantasy", "Fantasy & Science Fiction",
		"Rettungskreuzer Ikarus", "Star Trek", "Darkover",
	},
	"fantasy": {
		"Fantasy", "Fantastik", "Fantasyroman", "High Fantasy", "Epic fantasy", "Urban Fantasy", "Dark Fantasy",
		"Sword and sorcery", "Romantasy", "Magie", "Magic", "Drachen", "Dragons", "Elfen", "Science Fiction & Fantasy",
		"Fantasy & Magic", "Fantasy & Science Fiction", "Scheibenwelt",
	},
	"dystopia": {
		"Dystopie", "Dystopia", "Dystopian", "Dystopian fiction", "Endzeit", "Postapokalypse", "Post-apocalyptic",
		"Apocalyptic", "Dystopien",
	},
	"romance": {
		"Liebesroman", "Liebesromane", "Liebe", "Romanze", "Romance", "Love stories", "Love story", "Liebesgeschichte",
		"Liebesgeschichten", "Belletristik/Liebesromane", "Contemporary romance", "Historical romance", "Romantik",
		"Romantic comedy", "Liebeskomödie", "Chick lit", "Herz-Schmerz", "Liebe & Romantik", "Romantasy",
		"Julia", "Romana", "Baccara", "Bacarra", "Bianca", "Tiffany", "Historical RomanHefte",
	},
	"erotica": {
		"Erotik", "Erotischer Roman", "Erotische Literatur", "Erotica", "Erotic fiction", "Erotic", "Dark Romance",
		"BDSM", "Sex",
	},
	"historical": {
		"Historischer Roman", "Historische Romane", "Historische Romane, Erzählungen", "Belletristik/Historische Romane, Erzählungen",
		"Historical fiction", "Historical", "Historical novel", "Mittelalter", "Mittelalterroman", "Antike",
		"Historische Krimis", "Historical mystery",
		"Historisch", "Historische", "Historischer Krimi", "Historische Kriminalromane", "Historical RomanHefte",
	},
	"family-saga": {
		"Familiensaga", "Familienroman", "Familienromane", "Saga", "Family saga", "Sagas", "Family life", "Generationenroman",
	},
	"war": {
		"Kriegsroman", "Kriegsromane", "Krieg", "War", "War fiction", "War & military", "Military fiction",
		"Zweiter Weltkrieg", "Erster Weltkrieg",
	},
	"adventure": {
		"Abenteuer", "Abenteuerroman", "Abenteuerromane", "Adventure", "Adventure fiction", "Action & adventure",
		"Action and adventure", "Seefahrt", "Piraten", "Pirates", "Sea stories",
	},
	"western": {"Western", "Westerns", "Wildwest", "Wild West", "Westernroman"},
	"humour": {
		"Humor", "Humour", "Humorous", "Humorous fiction", "Satire", "Komödie", "Comedy", "Lustiges",
		"Belletristik/Humor", "Humoristische Romane", "Parodie", "Scheibenwelt",
	},
	"short-stories": {
		"Erzählungen", "Kurzgeschichten", "Short stories", "Short story", "Novellen", "Anthologie", "Anthologies",
		"Anthology", "Erzählband", "Erzählung",
	},
	"myths": {
		"Märchen", "Sagen", "Legenden", "Mythen", "Mythologie", "Märchen, Sagen, Legenden", "Fairy tales", "Folklore",
		"Mythology", "Legends", "Myths", "Fables", "Fabeln", "Belletristik/Märchen, Sagen, Legenden",
	},
	"poetry": {"Lyrik", "Gedichte", "Poesie", "Poetry", "Poems", "Verse", "Belletristik/Lyrik, Dramatik"},
	"drama":  {"Drama", "Dramen", "Dramatik", "Theater", "Theaterstück", "Plays", "Belletristik/Lyrik, Dramatik"},
	"comics": {
		"Comic", "Comics", "Comics & graphic novels", "Graphic Novel", "Graphic novels", "Manga", "Bildergeschichte",
		"Cartoons", "Comic strips",
	},
	"children": {
		"Kinderbuch", "Kinderbücher", "Kinder", "Bilderbuch", "Bilderbücher", "Kinderliteratur", "Erstleser",
		"Kinder- und Jugendbücher", "Kinderbücher bis 11 Jahre", "Children", "Children's", "Children's books",
		"Children's fiction", "Children's stories", "Juvenile fiction", "Juvenile", "Picture books", "Vorlesebuch",
	},
	"young-adult": {
		"Jugendbuch", "Jugendbücher", "Jugendliteratur", "Jugendroman", "Jugendbücher ab 12 Jahre", "Young adult",
		"Young adult fiction", "YA", "Teen", "Teenager", "New Adult", "All Age", "Jugend",
	},

	"biography": {
		"Biografie", "Biographie", "Biografien", "Biographien", "Autobiografie", "Autobiographie", "Memoiren",
		"Erinnerungen", "Biography", "Biographies", "Biography & autobiography", "Autobiography", "Memoir", "Memoirs",
		"Lebenserinnerungen", "Briefe", "Tagebücher", "Diaries", "Letters",
	},
	"true-crime": {"True Crime", "Wahre Verbrechen", "Kriminalfälle", "Kriminalfall"},
	"history": {
		"Geschichte", "Geschichte allgemein", "Historisches Sachbuch", "Zeitgeschichte", "History", "World history",
		"Military history", "Militärgeschichte", "Archäologie", "Archaeology", "Kulturgeschichte", "Antike Geschichte",
		"Nationalsozialismus", "Drittes Reich", "Sachbuch/Geschichte", "Geschichtswissenschaft",
	},
	"society": {
		"Politik", "Gesellschaft", "Politik & Gesellschaft", "Politik, Gesellschaft", "Soziologie", "Zeitgeschehen",
		"Gesellschaftskritik", "Politics", "Political science", "Social science", "Sociology", "Current affairs",
		"Feminismus", "Feminism", "Journalism", "Journalismus", "Medien", "Gesellschaft und Politik",
	},
	"business": {
		"Wirtschaft", "Wirtschaftswissenschaften", "Ökonomie", "Finanzen", "Geld", "Management", "Karriere", "Beruf",
		"Business", "Business & economics", "Economics", "Finance", "Investing", "Marketing", "Leadership",
		"Unternehmensführung", "Börse",
	},
	"law":        {"Recht", "Jura", "Rechtswissenschaft", "Law", "Legal", "Gesetze", "Steuern", "Steuerrecht"},
	"philosophy": {"Philosophie", "Philosophy", "Ethik", "Ethics", "Stoizismus", "Stoicism", "Existentialismus"},
	"religion": {
		"Religion", "Religionen", "Christentum", "Theologie", "Glaube", "Bibel", "Spiritualität", "Esoterik",
		"Meditation", "Buddhismus", "Islam", "Judentum", "Christianity", "Spirituality", "Theology", "Bible",
		"Body, mind & spirit", "Religion & Glaube",
	},
	"psychology": {
		"Psychologie", "Psychology", "Psychotherapie", "Psychotherapy", "Psychiatrie", "Neurowissenschaften",
		"Neuroscience", "Psychoanalyse",
	},
	"science": {
		"Naturwissenschaften", "Wissenschaft", "Physik", "Chemie", "Biologie", "Astronomie", "Mathematik", "Science",
		"Popular science", "Populärwissenschaft", "Physics", "Chemistry", "Biology", "Astronomy", "Mathematics",
		"Evolution", "Kosmologie", "Cosmology", "Naturwissenschaften, Medizin, Informatik, Technik",
	},
	"health": {
		"Gesundheit", "Medizin", "Ernährung", "Fitness", "Abnehmen", "Diät", "Health", "Health & fitness", "Medicine",
		"Medical", "Nutrition", "Diet", "Yoga", "Gesundheit & Ernährung", "Naturheilkunde",
	},
	"technology": {
		"Technik", "Ingenieurwesen", "Elektronik", "Elektrotechnik", "Maschinenbau", "Technology", "Engineering",
		"Technology & engineering", "Electronics", "Fahrzeuge", "Autos", "Luftfahrt", "Aviation",
	},
	"computing": {
		"Informatik", "EDV", "Computer", "Programmierung", "Software", "Internet", "Computers", "Programming",
		"Computer science", "Künstliche Intelligenz", "Artificial intelligence", "Datenbanken", "Linux",
		"Software development", "Web development",
	},
	"self-help": {
		"Ratgeber", "Lebenshilfe", "Selbsthilfe", "Persönlichkeitsentwicklung", "Erfolg", "Glück", "Partnerschaft",
		"Beziehung", "Erziehung", "Eltern", "Elternratgeber", "Self-help", "Self help", "Personal development",
		"Personal growth", "Self-improvement", "Relationships", "Parenting", "Family & relationships", "Motivation",
		"Achtsamkeit", "Mindfulness", "Lebensführung",
	},
	"cooking": {
		"Kochen", "Kochbuch", "Kochbücher", "Rezepte", "Backen", "Essen und Trinken", "Essen & Trinken", "Küche",
		"Vegetarisch", "Vegan", "Cooking", "Cookbook", "Cookbooks", "Cookery", "Recipes", "Baking", "Food", "Wein",
		"Wine", "Kochen & Genießen", "Getränke",
	},
	"travel": {
		"Reisen", "Reise", "Reiseführer", "Reisebericht", "Reiseberichte", "Reiseliteratur", "Travel", "Travel writing",
		"Travel guide", "Travel guides", "Länder", "Städte", "Wandern", "Hiking",
	},
	"nature": {
		"Natur", "Umwelt", "Tiere", "Pflanzen", "Klima", "Klimawandel", "Ökologie", "Naturschutz", "Nature",
		"Environment", "Animals", "Plants", "Climate change", "Ecology", "Natur & Umwelt",
	},
	"arts": {
		"Kunst", "Musik", "Film", "Fotografie", "Architektur", "Malerei", "Design", "Art", "Arts", "Music",
		"Photography", "Architecture", "Painting", "Performing arts", "Kunst & Kultur", "Kultur", "Tanz", "Dance",
		"Kunst, Musik", "Kino",
	},
	"language": {
		"Sprache", "Sprachen", "Sprachwissenschaft", "Linguistik", "Literaturwissenschaft", "Germanistik",
		"Wörterbuch", "Wörterbücher", "Sprachkurs", "Fremdsprachen", "Language", "Languages", "Linguistics",
		"Literary criticism", "Literary studies", "Language arts & disciplines", "Foreign language study",
		"Dictionaries", "Grammatik", "Grammar", "Schreiben", "Writing",
	},
	"sport": {
		"Sport", "Sports", "Sports & recreation", "Fußball", "Football", "Soccer", "Laufen", "Running", "Radsport",
		"Cycling", "Bergsteigen", "Klettern", "Climbing", "Schach", "Chess", "Kampfsport", "Martial arts",
	},
	"hobbies": {
		"Hobby", "Hobbys", "Freizeit", "Basteln", "Handarbeit", "Handarbeiten", "Stricken", "Nähen", "Garten",
		"Gärtnern", "Heimwerken", "Wohnen", "Sammeln", "Spiele", "Crafts & hobbies", "Crafts", "Gardening",
		"House & home", "Games", "Games & activities", "DIY", "Do-it-yourself", "Hobby & Freizeit", "Pets", "Haustiere",
	},
}

// table is Spellings by the key of each spelling.
var table = func() map[string][]string {
	m := map[string][]string{}
	for genre, spellings := range Spellings {
		for _, s := range spellings {
			k := catalog.Key(s)
			if k != "" && !slices.Contains(m[k], genre) {
				m[k] = append(m[k], genre)
			}
		}
	}
	for _, gs := range m {
		sort.Strings(gs)
	}
	return m
}()

func tableKeys() []string {
	keys := make([]string, 0, len(table))
	for k := range table {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// wrapperPrefixes and wrapperSuffixes are what sources put around the tags
// they write ("Science Fiction (st)", "Frei - Fantasy"), compared without
// regard to case. Only these come off, not any bracket:
// "Gegenwartsliteratur (ab 1945)" is a spelling of its own.
var (
	wrapperPrefixes = []string{"Frei -"}
	wrapperSuffixes = []string{"(st)"}
)

func unwrap(tag string) string {
	tag = strings.TrimSpace(tag)
	for {
		t := tag
		for _, p := range wrapperPrefixes {
			if len(t) >= len(p) && strings.EqualFold(t[:len(p)], p) {
				t = strings.TrimSpace(t[len(p):])
			}
		}
		for _, s := range wrapperSuffixes {
			if len(t) >= len(s) && strings.EqualFold(t[len(t)-len(s):], s) {
				t = strings.TrimSpace(t[:len(t)-len(s)])
			}
		}
		if t == tag {
			return t
		}
		tag = t
	}
}

// generic are tags that rightly give no genre: what a book is in general,
// its form, a shop's or a reader's own marks, publishers and countries.
// genre-check leaves them, and centuries, out of the tags it asks to map.
var generic = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range []string{
		"Roman", "Romane", "Novel", "Novels", "General Fiction", "Fiction", "General", "Allgemein", "Belletristik",
		"Neue Literatur", "Unterhaltung", "Unterhaltungsliteratur", "Literature & Fiction", "Sachbuch", "Sachbücher",
		"Nonfiction", "Non-fiction", "Fachbuch", "Fachbücher", "eBook", "E-Book", "eBooks", "E-Books", "Kindle eBooks",
		"Retail", "Foreign Languages", "Fremdsprachige Bücher", "Deutsch", "German", "Englisch", "English",
		"Bestseller", "Bestsellers", "Spiegel-Bestseller", "Gelesen", "Ungelesen", "Read", "To read", "Wunschliste",
		"Leihbuch", "Favoriten", "Favorites",
		"Heyne", "Goldmann", "Bastei Lübbe", "Lübbe", "Rowohlt", "Fischer", "S. Fischer", "Piper", "Droemer",
		"Knaur", "Droemer Knaur", "Blanvalet", "dtv", "Diogenes", "Suhrkamp", "Hanser", "Kiepenheuer & Witsch",
		"Ullstein", "Aufbau", "Penguin", "Random House", "Penguin Random House", "HarperCollins", "Macmillan",
		"Simon & Schuster", "Hachette",
		"Deutschland", "Germany", "Österreich", "Austria", "Schweiz", "Switzerland", "USA", "United States",
		"Amerika", "England", "Großbritannien", "Great Britain", "Frankreich", "France", "Italien", "Italy",
		"Spanien", "Spain", "Schweden", "Sweden", "Norwegen", "Norway", "Dänemark", "Denmark", "Russland",
		"Russia", "Irland", "Ireland", "Schottland", "Scotland", "Japan", "China", "Kanada", "Canada",
		"Australien", "Australia",
	} {
		m[catalog.Key(s)] = true
	}
	return m
}()

var century = regexp.MustCompile(`^\d{1,2}(st|nd|rd|th)? (jahrhundert|century)$`)

// isGeneric says whether a tag is one that rightly gives no genre.
func isGeneric(tag string) bool {
	k := catalog.Key(unwrap(tag))
	return generic[k] || century.MatchString(k)
}

// worthMapping says whether genre-check should list a tag to be added to
// Spellings.
func worthMapping(tag string) bool {
	return len(FromTag(tag)) == 0 && !isGeneric(tag)
}

// FromTag returns the genres a tag means, once the wrappers are off: those
// of the whole tag where the table has it; otherwise, for a tag of parts split at "/" or ">", the most
// specific part the table knows (the last but the first, "General" and
// such are in none), and the first part as well when it is known, as in
// "Juvenile Fiction / Fantasy & Magic". A part the table does not know is
// also tried as a list, split at commas, "&", "and" and "und".
func FromTag(tag string) []string {
	tag = unwrap(tag)
	if gs, ok := table[catalog.Key(tag)]; ok {
		return slices.Clone(gs)
	}
	parts := strings.FieldsFunc(tag, func(r rune) bool { return r == '/' || r == '>' || r == '|' })
	if len(parts) < 2 {
		return fromPart(tag)
	}
	out := fromPart(parts[0])
	for i := len(parts) - 1; i >= 1; i-- {
		if gs := fromPart(parts[i]); len(gs) > 0 {
			out = append(out, gs...)
			break
		}
	}
	return unique(out)
}

func fromPart(part string) []string {
	if gs, ok := table[catalog.Key(part)]; ok {
		return slices.Clone(gs)
	}
	var out []string
	for _, piece := range splitList(part) {
		out = append(out, table[catalog.Key(piece)]...)
	}
	return unique(out)
}

func splitList(s string) []string {
	s = strings.NewReplacer("&", ",", ";", ",", " und ", ",", " and ", ",", " + ", ",").Replace(s)
	return strings.Split(s, ",")
}

// FromTags returns the genres all of a book's tags mean, in List's order.
func FromTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		out = append(out, FromTag(t)...)
	}
	out = unique(out)
	order := map[string]int{}
	for i, g := range List {
		order[g.Key] = i
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}

func unique(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
