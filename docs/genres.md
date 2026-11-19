# Genres

GOtome gives books genres of one fixed list (#175,
`backend/internal/genres/genres.go`): 23 kinds of fiction and 21 areas of
non-fiction, each with an English and a German name and a short
description in both languages. A book's tags stay as they are; its genres
are kept beside them.

## Where genres come from

- **Tags.** `Spellings` (`backend/internal/genres/tags.go`) lists, per
  genre, the tags that mean it, in both languages and as the trade
  headings write them. Tags are compared as names are elsewhere: case,
  accents and punctuation do not matter. A heading of parts
  ("Belletristik/Kriminalromane", "Fiction / Fantasy / General") is read
  part by part. What a source writes around its tags is taken off first:
  a trailing "(st)" and a leading "Frei - " ("Science Fiction (st)" is
  Science Fiction; `wrapperPrefixes`, `wrapperSuffixes`). Other brackets
  stay, as "Gegenwartsliteratur (ab 1945)" is a spelling of its own. A tag
  in none of the lists gives no genre and stays a tag. Add spellings
  there; `gotome genre-check` lists the tags of the most books that give
  none, leaving out those that rightly give none (`generic` in the same
  file: "Roman", "Belletristik", "Sachbuch", "eBook", a shop's and a
  reader's own marks, publishers, countries, and centuries).
- **Suggestions.** By the ensemble (`ensemble`, #528, below), from a
  book's words, its vectors and the genres of its nearest books
  together; or from the neighbours alone, the genres at least 40% of its
  nearest books in the library have (`neighbours`, #66). At most three
  genres a book, none its tags already give. A suggestion waits under
  Genres in the app until a person accepts or rejects it; a rejected one
  is not suggested for that book again.

**Genres for books** (`genres.suggest`) in the settings is `off`, `tags`,
`neighbours` or `ensemble`, the default. The library's filter finds the
genres of tags and those accepted, never a suggestion that waits. Before
0.26 the default was `off`, and the setting also took `vector` and
`combined`; an installation set to either was moved to `ensemble`, and
`genre-check` still measures them.

## The ensemble

The ensemble (`backend/internal/genres/ensemble.go`,
`docs/decisions/genre-detection.md`) asks four inputs how likely each genre
is, each where the book has it:

- **words**: the word weights of the book's whole text
  (`file_words`, below), for a language GOtome has a table of;
- **content**: a head on the book's content vector;
- **metadata**: a head on the vector of its title, authors and
  description;
- **neighbours**: the share of its nearest books in the library that have
  the genre, as `neighbours` counts it.

A stacker for the set of inputs the book has turns them into one
probability per genre. A genre is suggested from the probability at which
the training books, scored by models that had not seen them, were right
four times in five (`TargetPrecision`); a genre that never got there is
not suggested at all. The review shows the probability, each input's, and
the book's words that weighed most.

What ships (`backend/internal/genres/shipped`, numbers only) is trained on
1,171 books of Project Gutenberg whose subject headings give one of 25
genres: adventure, arts, biography, children, crime, drama, fantasy,
historical, history, humour, myths, nature, philosophy, poetry,
psychology, religion, romance, science, science fiction, self-help, short
stories, society, travel, war and western. Other genres come from tags
only. The heads are for multilingual-e5-small, and the word table is for
English: a library embedded with bge-m3, or of German books, is served by
the inputs that remain (for bge-m3 the neighbours and, in English, the
words). On a fifth of the corpus held out, the ensemble gave P 0.78, R
0.56, F1 0.65 and a genre to 72% of books; the content vector alone P
0.73, R 0.42. On a modern German library it was 0.6 to 0.7 precise
(the decision record): suggestions only wait for review, so that costs
review time and changes nothing by itself.

A genre with fewer than 15 Gutenberg books is not shipped: horror,
thriller, contemporary, classics, dystopia, erotica, young adult, family
saga and comics have too few for a head or a threshold, so they come from
tags and from what a library learns itself. `spikes/genres/run.sh ship`
(after `run.sh gutenberg`, which makes the records with Project Gutenberg's
header and licence left out) trains the whole default again from the public
corpus: heads, stackers, thresholds and the English word table, with fixed
orders and no random numbers, so two runs, on any number of threads, write
the same bytes. About 30 seconds on four threads once the records exist, and
about two minutes to make them. The files carry a `version` (a hash of the
numbers), the hash of the corpus files it was trained on and the books it
used per genre; they hold no title, author or text. Only multilingual-e5-small
has heads: another model needs the corpus's 16 passages embedded with it, and
an entry in the harness. An installation that finds a new shipped `version`
replaces the shipped part and keeps what it learned itself.

## Measuring before switching suggestions on

The thresholds in the code are guesses until they are measured on a
library. `gotome genre-check` takes a sample of books whose tags give a
genre, hides those genres (their metadata vector is made again without
the tags) and counts how many each method gives back: precision and
recall per method and per genre, with the thresholds swept. It writes
nothing, and its queries are light: one row per book and per tag, then
for each book of the sample its vectors and one look at the vectors of
its library.

Once the vectors are complete (the overview's "This machine" and
`GET /embedding/status`), run on the installation:

```
docker compose exec app gotome genre-check -n 300
```

(`make genre-check ARGS="-n 300"` for a development stack.) Options:
`-n` books in the sample, `-precision` the precision a genre's threshold
must reach (0.8), `-neighbours=false` to leave out the look at the
library's vectors, `-ensemble=false` to leave out the ensemble, `-train`
how many tagged books the ensemble is trained on (1,000, the sample among
them), `-json` for the whole report.

The ensemble is measured as it runs: a genre is given where its calibrated
threshold is reached, at most three a book, and a genre it has no threshold
for abstains. The tagged books are cut into five folds by a hash of their
ID, and each fold is judged by an ensemble trained, as the shipped one is
(`TrainEnsemble`), on the other four, so no book is judged by a model that
has seen it. The sample is the first `-n` books by that hash. The report
gives the ensemble's precision, recall, F1 and the share of books given a
genre per genre as it decided and with one threshold swept, and each input
alone (`ensemble-words`, `-content`, `-metadata`, `-neighbours`, at 0.5 and
swept) beside it. Only genres with at least 20 training books are trained,
a language needs 100 for a word table, and what the report names is genre
keys and numbers, never a book.

The report gives the share of books with a genre from their tags (before)
and, per method, the share of the sample given one at each threshold
(after). From it, set `DefaultThreshold`, each `Genre.Threshold` and
`NeighbourShare`, choose the method, and only then switch the setting on.

The figures `genre-check` should come near on the shipped training's
corpus (the spike's, a fifth held out; `genre-detection.md`), and on any
other library what its own run gives:

| Method | Precision | Recall | F1 | Given a genre |
|---|---|---|---|---|
| ensemble | 0.78 | 0.56 | 0.65 | 72% |
| ensemble-content | 0.73 | 0.42 | | |

## What was measured

On the owner's library (48,560 books, all with bge-m3 vectors, 13,058
tagged, 8,077 with a genre from their tags), `genre-check -n 50` gave
(#308; P precision, R recall, "given" the share of the sample given any
genre):

| Method | At the thresholds then | Best single threshold |
|---|---|---|
| vector-content | P 0.11, R 0.25, F1 0.15, 98% given | no better |
| vector-metadata | P 0.12, R 0.26, F1 0.16 | F1 0.18 |
| vector | P 0.12, R 0.25, F1 0.16 | F1 0.19 |
| neighbours, share 0.5 | P 0.79, R 0.35, F1 0.49, 50% given | |
| neighbours, share 0.4 | P 0.74, R 0.43, F1 0.54, 62% given | (now `NeighbourShare`) |

`neighbours` was the method to use before the ensemble. `combined`
took the neighbours' answer wherever at least `MinNeighbours` of the
nearest books have genres, so on a library with tags it gave what
`neighbours` gives, and asked the vectors only for a book among untagged
books.

### Why the vectors are no better than guessing

On the owner's library the scores of every genre lay within about 0.1 of
each other, and no threshold separated them. bge-m3 could not be run
here (its files are not downloaded on this machine), so the cause was
measured on multilingual-e5-small, which the spike's corpus has vectors
of (#15: 2,011 Project Gutenberg books in English, German and French,
1,056 of whose subject headings give a genre, a book's content vector of
16 passages): the best genre of each book against the genres of its
headings.

| Scores | Best genre right | Best F1 |
|---|---|---|
| always the commonest genre (short stories) | 0.23 | |
| content, margins as now | 0.09 | 0.09 |
| content, each genre's margin centred over the books | 0.19 | 0.19 |
| content, each genre's margin as a z-score over the books | 0.23 | 0.22 |
| metadata (title and authors), margins as now | 0.24 | 0.24 |
| prototypes of the names alone, or with `passage: ` | 0.04–0.20 | 0.09–0.21 |
| the mean vector of the tagged books of each genre | 0.49 | 0.38 |
| neighbours (40 candidates, 10 with genres) | 0.60 | 0.57 |

A genre's description is one sentence; a book's vector is the mean of
passages of its prose. To the model every passage of a novel is about as
far from every such sentence (the margins spread over 0.04 on e5), and
what little differs is the style of the sentence more than the genre: two
genres (horror and myths) were the nearest for 60% of the books
whatever they were about. Taking a genre's own average off (centring,
z-scores) removes that, but then the vectors rank genres no better than
always guessing the commonest. Other prototype texts and the `passage: `
prefix did not help. The prefix is not the cause for bge-m3: it takes
none, and none is given (`embed.BGEM3`). The vectors carry genre, as the
neighbours show, but only among books: the mean of a genre's tagged books
finds the right genre five times as often as its description, and the
nearest books more often still. That is what `neighbours` uses, so the zero-shot methods
stay for measuring and are not recommended.

After an upgrade, run `genre-check -n 300` and compare with the table.

## Word counts

For word weights from a book's whole text (`docs/decisions/genre-detection.md`),
GOtome counts the words of every book's text while it chunks it, and keeps the
counts in the table `file_words` (a book's primary text file, its words
lower-cased, of 3 to 25 letters, and how often each occurs), which the
ensemble's word weights read. A book counted after its genres were
looked at is looked at again. They are not backed up: a restore reads every text
again and counts it again.

What it costs, measured on the public corpus (books of about 380,000
characters): counting takes about 5 ms a book while the text is in memory, and
storing the counts about 3 ms and 11 KB a book, so about 0.5 GB for 48,560
books. A word is kept when it occurs at least twice in the book
(`wordcount.MinCount`): words that occur once are 60% of a book's distinct
words and half of the size, and leaving them out costs 0.008 of the word
weights' F1 on the corpus (0.652 against 0.660; keeping only words that occur
three times or more gives 0.644 at 8 KB). Books chunked before counting
existed are counted by the job "Count the words of the books' text", which
reads each file again (about 45 ms a book, about 40 minutes for 48,560 books on
one CPU) and goes on where it stopped. Project Gutenberg's header and licence
are left out; words are counted as written, not stemmed, so a stem can be
chosen later without reading anything again. Changing the rule means raising
`wordcount.Version`, which counts every file again.

### Adapting to the library

Once the library has at least 20 books with a genre (from tags, or a
suggestion a person accepted), a background job trains the ensemble on them
(`genres.adapt`, `Service.Adapt`, `adapt.go`): every day, and sooner when
the number of such books changed by a fifth, the shipped default changed
or another embedding model was chosen. The heads and the word tables of the
books' languages (at most 4,000 books a language) are merged with the
shipped ones, each genre weighed by the books that taught it; what ships
keeps its document frequencies and counts, not its books. A set of inputs
with at least 100 labelled books gets its own stacker and thresholds,
calibrated on books the models had not seen; the other sets keep the shipped
ones. A library without genres keeps the shipped ensemble exactly.

The result is one row, `genre_adaptation`, for the shipped version it was
trained beside: when that changes, the row is not used until the job has
trained again. It is derived and not in a backup. The same books give the
same numbers, and a book is looked at again only when they change. Reading
the books stops at the job's time and goes on in the next run; nothing is
stored before the training is whole.

