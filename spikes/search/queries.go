package main

// Kind is what a query exercises.
type Kind string

const (
	Stemmed  Kind = "stemmed"
	Phrase   Kind = "phrase"
	Fuzzy    Kind = "fuzzy"
	Filtered Kind = "filtered"
	Snippets Kind = "snippets"
	// Repaired are the fuzzy queries with their words looked up in a
	// vocabulary first and searched like stemmed ones, with snippets.
	Repaired Kind = "repaired"
)

// Kinds in the order the report lists them.
var Kinds = []Kind{Stemmed, Phrase, Fuzzy, Filtered, Snippets, Repaired}

func (k Kind) snippets() bool { return k == Snippets || k == Repaired }

// Query is one search. All words must match; a phrase must match in order.
// Fuzzy queries carry one typo per word, which one edit repairs.
type Query struct {
	Kind Kind
	Text string
	// Want is the word a hit is expected to contain, to check stemming
	// and typo repair by eye in the report.
	Want string
}

// Libraries is how many libraries the books are spread over, by ID.
const Libraries = 20

// FilterLibraries is the library filter of Filtered queries: a tenth of
// the books.
var FilterLibraries = []int32{3, 11}

// Queries mix rare and common words in the corpus's three languages.
var Queries = []Query{
	{Stemmed, "whales harpooned", "harpoon"},
	{Stemmed, "running horses", "horse"},
	{Stemmed, "ancient temples ruins", "temple"},
	{Stemmed, "detective murdered", "detective"},
	{Stemmed, "love letters", "letter"},
	{Stemmed, "Häuser Flüsse", "Haus"},
	{Stemmed, "verlorene Liebe", "Liebe"},
	{Stemmed, "Kinder spielten", "Kind"},
	{Stemmed, "maison jardin", "jardin"},
	{Stemmed, "morning", "morning"},
	{Phrase, "white whale", "white whale"},
	{Phrase, "once upon a time", "once upon a time"},
	{Phrase, "in the morning", "in the morning"},
	{Phrase, "der alte Mann", "alte Mann"},
	{Phrase, "es war einmal", "war einmal"},
	{Fuzzy, "detectve", "detective"},
	{Fuzzy, "whael harpon", "whale"},
	{Fuzzy, "Kinnder", "Kinder"},
	{Fuzzy, "elefant", "elephant"},
	{Fuzzy, "jardn", "jardin"},
	{Filtered, "whales harpooned", "harpoon"},
	{Filtered, "running horses", "horse"},
	{Filtered, "verlorene Liebe", "Liebe"},
	{Filtered, "morning", "morning"},
	{Snippets, "whales harpooned", "harpoon"},
	{Snippets, "running horses", "horse"},
	{Snippets, "verlorene Liebe", "Liebe"},
	{Snippets, "morning", "morning"},
	{Repaired, "detectve", "detective"},
	{Repaired, "whael harpon", "whale"},
	{Repaired, "Kinnder", "Kinder"},
	{Repaired, "elefant", "elephant"},
	{Repaired, "jardn", "jardin"},
}

// Hit is one chunk found, with a snippet when the query asked for them.
type Hit struct {
	ID      int64   `json:"id"`
	Book    int     `json:"book"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// TopK is how many chunks a query returns.
const TopK = 10
