// Package genres gives books genres of a fixed list (#175): from their tags,
// through a table of the spellings that mean each genre, and as suggestions
// from their vectors, compared with each genre's description (zero-shot)
// and with the genres of their nearest books. Nothing is written to a
// book's tags; a genre is a row of book_genres, and a suggestion waits for
// a person to accept or reject it.
package genres

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Genre is one entry of the list.
type Genre struct {
	// Key names the genre in the database, the API and filters. It stays.
	Key string
	// Thema is the genre's code in EDItEUR's Thema scheme, where it is
	// certain; empty otherwise.
	Thema string
	// English and German are the names; the web app has its own copies in
	// its translations, which a test holds to these.
	English, German string
	// Fiction tells the list's two halves apart.
	Fiction bool
	// About is what the model compares a book with, in English and German.
	About, Über string
	// Threshold is the zero-shot score from which a book is given the
	// genre; zero is DefaultThreshold. Set from genre-check's numbers.
	Threshold float64
}

// List is every genre there is: fiction first, then the main areas of
// non-fiction.
var List = []Genre{
	{Key: "contemporary", Thema: "FA", English: "Contemporary fiction", German: "Gegenwartsliteratur", Fiction: true,
		About: "Literary novels about people's lives, relationships and society today.",
		Über:  "Romane über das Leben, die Beziehungen und die Gesellschaft von heute."},
	{Key: "classics", Thema: "FB", English: "Classics", German: "Klassiker", Fiction: true,
		About: "Classic novels and stories of world literature, written before the middle of the twentieth century.",
		Über:  "Klassische Romane und Erzählungen der Weltliteratur, geschrieben vor der Mitte des zwanzigsten Jahrhunderts."},
	{Key: "crime", Thema: "FF", English: "Crime & mystery", German: "Krimi", Fiction: true,
		About: "A detective, an inspector or an amateur investigates a murder or another crime and finds out who did it.",
		Über:  "Ein Kommissar, ein Detektiv oder ein Laie ermittelt in einem Mord oder einem anderen Verbrechen und findet den Täter."},
	{Key: "thriller", Thema: "FH", English: "Thriller & suspense", German: "Thriller", Fiction: true,
		About: "A race against time and danger: conspiracies, agents, kidnappings, a killer on the loose, suspense to the end.",
		Über:  "Ein Wettlauf gegen die Zeit und die Gefahr: Verschwörungen, Agenten, Entführungen, ein Killer, Spannung bis zum Schluss."},
	{Key: "horror", Thema: "FK", English: "Horror", German: "Horror", Fiction: true,
		About: "Ghosts, demons, monsters and the supernatural, told to frighten.",
		Über:  "Geister, Dämonen, Monster und das Übernatürliche, erzählt, um das Fürchten zu lehren."},
	{Key: "science-fiction", Thema: "FL", English: "Science fiction", German: "Science-Fiction", Fiction: true,
		About: "The future, space travel, other planets, aliens, robots and technology that does not exist yet.",
		Über:  "Die Zukunft, Raumfahrt, fremde Planeten, Außerirdische, Roboter und Technik, die es noch nicht gibt."},
	{Key: "fantasy", Thema: "FM", English: "Fantasy", German: "Fantasy", Fiction: true,
		About: "Magic, dragons, elves and invented worlds with kingdoms, quests and battles between good and evil.",
		Über:  "Magie, Drachen, Elfen und erfundene Welten mit Königreichen, Abenteuern und dem Kampf von Gut gegen Böse."},
	{Key: "dystopia", English: "Dystopia", German: "Dystopie", Fiction: true,
		About: "A society of the future gone wrong: surveillance, oppression, survival after a collapse.",
		Über:  "Eine Gesellschaft der Zukunft, die misslungen ist: Überwachung, Unterdrückung, Überleben nach dem Zusammenbruch."},
	{Key: "romance", Thema: "FR", English: "Romance", German: "Liebesroman", Fiction: true,
		About: "Two people fall in love; the story follows their romance to a happy ending.",
		Über:  "Zwei Menschen verlieben sich; die Geschichte folgt ihrer Liebe bis zum glücklichen Ende."},
	{Key: "erotica", Thema: "FP", English: "Erotic fiction", German: "Erotik", Fiction: true,
		About: "Stories about desire and sex, told explicitly.",
		Über:  "Geschichten über Begehren und Sex, offen erzählt."},
	{Key: "historical", Thema: "FV", English: "Historical fiction", German: "Historischer Roman", Fiction: true,
		About: "A novel set in the past: the Middle Ages, antiquity or another century, with its people and events.",
		Über:  "Ein Roman in der Vergangenheit: im Mittelalter, in der Antike oder einem anderen Jahrhundert, mit seinen Menschen und Ereignissen."},
	{Key: "family-saga", English: "Family saga", German: "Familiensaga", Fiction: true,
		About: "The fortunes of one family over several generations.",
		Über:  "Das Schicksal einer Familie über mehrere Generationen."},
	{Key: "war", English: "War fiction", German: "Kriegsroman", Fiction: true,
		About: "Soldiers, battles and the lives of people in a war.",
		Über:  "Soldaten, Schlachten und das Leben der Menschen im Krieg."},
	{Key: "adventure", Thema: "FJ", English: "Adventure", German: "Abenteuer", Fiction: true,
		About: "Journeys, expeditions, pirates and explorers in dangerous places.",
		Über:  "Reisen, Expeditionen, Piraten und Entdecker an gefährlichen Orten."},
	{Key: "western", English: "Western", German: "Western", Fiction: true,
		About: "Cowboys, sheriffs and settlers in the American Wild West.",
		Über:  "Cowboys, Sheriffs und Siedler im Wilden Westen Amerikas."},
	{Key: "humour", Thema: "FU", English: "Humour", German: "Humor", Fiction: true,
		About: "Funny stories and satire, written to make people laugh.",
		Über:  "Lustige Geschichten und Satire, geschrieben, um zum Lachen zu bringen."},
	{Key: "short-stories", English: "Short stories", German: "Erzählungen", Fiction: true,
		About: "A collection of short stories or novellas.",
		Über:  "Eine Sammlung von Kurzgeschichten oder Novellen."},
	{Key: "myths", English: "Myths, legends & fairy tales", German: "Märchen & Sagen", Fiction: true,
		About: "Fairy tales, myths, legends and folk tales retold.",
		Über:  "Märchen, Mythen, Sagen und Legenden, neu erzählt."},
	{Key: "poetry", Thema: "DC", English: "Poetry", German: "Lyrik", Fiction: true,
		About: "Poems and verse.",
		Über:  "Gedichte und Verse."},
	{Key: "drama", Thema: "DD", English: "Plays", German: "Drama", Fiction: true,
		About: "Plays written for the stage.",
		Über:  "Theaterstücke, geschrieben für die Bühne."},
	{Key: "comics", Thema: "X", English: "Comics & graphic novels", German: "Comics & Graphic Novels", Fiction: true,
		About: "Stories told in pictures: comics, manga and graphic novels.",
		Über:  "Geschichten in Bildern: Comics, Manga und Graphic Novels."},
	{Key: "children", English: "Children's books", German: "Kinderbuch", Fiction: true,
		About: "Stories for children: picture books and books for young readers.",
		Über:  "Geschichten für Kinder: Bilderbücher und Bücher für junge Leser."},
	{Key: "young-adult", English: "Young adult", German: "Jugendbuch", Fiction: true,
		About: "Novels for teenagers about growing up, first love and finding one's place.",
		Über:  "Romane für Jugendliche über das Erwachsenwerden, die erste Liebe und den eigenen Weg."},

	{Key: "biography", English: "Biography & memoir", German: "Biografie & Erinnerungen",
		About: "The life of a real person, told by themselves or by someone else.",
		Über:  "Das Leben eines wirklichen Menschen, von ihm selbst oder von anderen erzählt."},
	{Key: "true-crime", English: "True crime", German: "True Crime",
		About: "Real crimes, criminals and trials, told as they happened.",
		Über:  "Wahre Verbrechen, Täter und Prozesse, so erzählt, wie sie geschahen."},
	{Key: "history", Thema: "NH", English: "History", German: "Geschichte",
		About: "Non-fiction about historical events, eras, wars and civilisations.",
		Über:  "Sachbuch über geschichtliche Ereignisse, Epochen, Kriege und Kulturen."},
	{Key: "society", Thema: "J", English: "Politics & society", German: "Politik & Gesellschaft",
		About: "Non-fiction about politics, current affairs, society and social questions.",
		Über:  "Sachbuch über Politik, Zeitgeschehen, Gesellschaft und soziale Fragen."},
	{Key: "business", Thema: "K", English: "Business & economics", German: "Wirtschaft",
		About: "Economics, finance, management, careers and money.",
		Über:  "Wirtschaft, Finanzen, Management, Beruf und Geld."},
	{Key: "law", Thema: "L", English: "Law", German: "Recht",
		About: "Law, legal systems and legal practice.",
		Über:  "Recht, Gesetze und juristische Praxis."},
	{Key: "philosophy", Thema: "QD", English: "Philosophy", German: "Philosophie",
		About: "Philosophy: thinking about knowledge, ethics, existence and the mind.",
		Über:  "Philosophie: Nachdenken über Erkenntnis, Ethik, das Dasein und den Geist."},
	{Key: "religion", Thema: "QR", English: "Religion & spirituality", German: "Religion & Spiritualität",
		About: "Religions, faith, the Bible, spirituality and meditation.",
		Über:  "Religionen, Glaube, die Bibel, Spiritualität und Meditation."},
	{Key: "psychology", Thema: "JM", English: "Psychology", German: "Psychologie",
		About: "How people think, feel and behave; the mind and mental health.",
		Über:  "Wie Menschen denken, fühlen und handeln; die Seele und seelische Gesundheit."},
	{Key: "science", Thema: "P", English: "Science & mathematics", German: "Naturwissenschaften & Mathematik",
		About: "Popular and academic science: physics, chemistry, biology, astronomy and mathematics.",
		Über:  "Wissenschaft für alle und für das Studium: Physik, Chemie, Biologie, Astronomie und Mathematik."},
	{Key: "health", English: "Health & medicine", German: "Gesundheit & Medizin",
		About: "Medicine, illness, nutrition, fitness and staying healthy.",
		Über:  "Medizin, Krankheiten, Ernährung, Fitness und gesund bleiben."},
	{Key: "technology", Thema: "T", English: "Technology & engineering", German: "Technik",
		About: "Engineering, machines, electronics and how technology works.",
		Über:  "Ingenieurwesen, Maschinen, Elektronik und wie Technik funktioniert."},
	{Key: "computing", Thema: "U", English: "Computing", German: "Informatik & EDV",
		About: "Computers, programming, software, networks and the internet.",
		Über:  "Computer, Programmierung, Software, Netzwerke und das Internet."},
	{Key: "self-help", Thema: "VS", English: "Self-help", German: "Ratgeber & Lebenshilfe",
		About: "Advice for one's own life: personal development, relationships, parenting, happiness and success.",
		Über:  "Rat für das eigene Leben: Persönlichkeitsentwicklung, Beziehungen, Erziehung, Glück und Erfolg."},
	{Key: "cooking", Thema: "WB", English: "Cooking & food", German: "Kochen & Genießen",
		About: "Cookbooks, recipes, baking and drinks.",
		Über:  "Kochbücher, Rezepte, Backen und Getränke."},
	{Key: "travel", Thema: "WT", English: "Travel", German: "Reisen",
		About: "Travel guides and travel writing about countries, cities and journeys.",
		Über:  "Reiseführer und Reiseberichte über Länder, Städte und Reisen."},
	{Key: "nature", English: "Nature & environment", German: "Natur & Umwelt",
		About: "Animals, plants, landscapes, the climate and protecting the environment.",
		Über:  "Tiere, Pflanzen, Landschaften, das Klima und der Schutz der Umwelt."},
	{Key: "arts", Thema: "A", English: "Arts, music & film", German: "Kunst, Musik & Film",
		About: "Painting, architecture, photography, music, film and theatre.",
		Über:  "Malerei, Architektur, Fotografie, Musik, Film und Theater."},
	{Key: "language", English: "Language & literary studies", German: "Sprache & Literaturwissenschaft",
		About: "Languages, linguistics, dictionaries and writing about literature.",
		Über:  "Sprachen, Sprachwissenschaft, Wörterbücher und Schriften über Literatur."},
	{Key: "sport", Thema: "S", English: "Sport", German: "Sport",
		About: "Sports, athletes, games and outdoor activities.",
		Über:  "Sportarten, Sportler, Spiele und Aktivitäten im Freien."},
	{Key: "hobbies", English: "Hobbies, crafts & home", German: "Hobby, Handwerk & Wohnen",
		About: "Crafts, gardening, do-it-yourself, collecting, games and the home.",
		Über:  "Basteln, Garten, Heimwerken, Sammeln, Spiele und das Zuhause."},
}

// DefaultThreshold is the zero-shot score from which a genre without a
// threshold of its own is given: how much nearer the genre is than the
// genres are on average (Margins). It is a guess until genre-check has
// measured one.
const DefaultThreshold = 0.05

// MaxGenres is how many genres a book is suggested at most.
const MaxGenres = 3

var byKey = func() map[string]Genre {
	m := make(map[string]Genre, len(List))
	for _, g := range List {
		m[g.Key] = g
	}
	return m
}()

// Get returns the genre of a key.
func Get(key string) (Genre, bool) {
	g, ok := byKey[key]
	return g, ok
}

// Keys are the keys of List, in its order.
func Keys() []string {
	out := make([]string, len(List))
	for i, g := range List {
		out[i] = g.Key
	}
	return out
}

func (g Genre) threshold() float64 {
	if g.Threshold > 0 {
		return g.Threshold
	}
	return DefaultThreshold
}

// texts are what the model is given for the genre: its description in
// either language, each embedded on its own.
func (g Genre) texts() []string {
	return []string{g.English + ". " + g.About, g.German + ". " + g.Über}
}

// Version is a hash of the list, its descriptions, thresholds, the tag
// table and the wrappers: a change has every book looked at again.
func Version() string {
	h := sha256.New()
	for _, g := range List {
		h.Write([]byte(strings.Join(append(g.texts(), g.Key), "\x00")))
		h.Write([]byte(strconv.FormatFloat(g.threshold(), 'g', -1, 64)))
	}
	for _, k := range tableKeys() {
		h.Write([]byte(k + "=" + strings.Join(table[k], ",") + "\n"))
	}
	h.Write([]byte(strings.Join(wrapperPrefixes, "\x00") + "\n" + strings.Join(wrapperSuffixes, "\x00")))
	return hex.EncodeToString(h.Sum(nil))[:12]
}
