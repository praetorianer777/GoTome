# Book details from metadata sources

GOtome reads what each file says about its book (title, authors, series,
description, identifiers, cover) and can fill in what is missing from
sources on the internet. Nothing a person typed is ever overwritten.

## The sources

| Source | Name in `metadata.providers` | Key | What it is good for |
|---|---|---|---|
| [Open Library](https://openlibrary.org) | `openlibrary` | none | Books by ISBN, title and author; covers; the works of an author. |
| [CrossRef](https://www.crossref.org) | `crossref` | none; a contact address helps | Scholarly books and papers, by DOI or ISBN. Set `metadata.contactEmail`: CrossRef serves requests that name one faster. |
| [Hardcover](https://hardcover.app) | `hardcover` | `metadata.hardcoverToken` | Current fiction, series and their order, the books of an author or a series. Without a token it is not asked. |
| [Deutsche Nationalbibliothek](https://www.dnb.de) | `dnb` | none | Books in German, by ISBN, title and author; the books of an author or a series, including titles announced by publishers with the month they are due. Not asked unless `metadata.providers` names it. |

To get a Hardcover token: sign in at hardcover.app, open the account
settings, **API**, and copy the token. Paste it into **Hardcover API
token** on GOtome's Settings page. Like every secret it is stored
encrypted and not shown again.

GOtome asks each source at a polite pace (Open Library every 400 ms,
CrossRef every 350 ms, Hardcover and the Deutsche Nationalbibliothek about
once a second, as each asks), and
keeps each answer for 30 days, so a lookup repeated soon asks nobody.
Keys are never part of what is kept.

`GOTOME_OFFLINE=true` asks no source at all. `metadata.providers` names
which are asked; leave one out to stop it.

## What happens to a new book

With `metadata.autoMatch` on (the default), every book is looked up once
its file is read: by its ISBNs and its DOI, and by title and author where
those find nothing. Each candidate gets a score: 1 for a shared
identifier, at most 0.99 otherwise. The title counts most, then the
authors, the year and the language. Where both the book and the
candidate say it, further signals add to or take from the score: a
different language multiplies it by 0.6 (a translation is not the book),
a page count within 10% (at least 15 pages) adds a little and one 40%
off takes a little, as does the same publisher or a different one, the
same series, and within it the same position (a different position takes
10 points: the next volume is not this book), and the same medium as the
book's files (an e-book against an EPUB) or another one (the hardback).
A side that says nothing about a signal changes nothing. Among equal
scores, candidates in the book's own language come first.

The weights are named constants in `backend/internal/metadata/score.go`.
Most sources' searches answer works, not editions, and give no language,
publisher, pages or medium to compare: the signals tell editions apart
where a source lists them, as the Deutsche Nationalbibliothek does.
Measured on 200 German books of a private library with an ISBN, looked
up without it at the DNB and Open Library (2026-11-11):

| | before | after |
|---|---|---|
| first candidate the book's own edition | 30% | 32% |
| another edition of the book first | (counted as wrong) | 20% |
| another book first | 30% (with the editions) | 6% |
| at 0.95: taken, own edition / another edition / another book | 77: 45 / – / 32 | 77: 50 / 25 / 0, and 2 of its work |

For a library mostly in German, put `dnb` first in `metadata.providers`
(`dnb,openlibrary,hardcover`): on that library Open Library, CrossRef and
Hardcover put the book's own edition first for one book in a hundred,
and another book for most. CrossRef knows scholarly books only; leave it
out of a library of fiction.

- A best candidate at or above `metadata.matchThreshold` (0.95) is taken,
  but only to fill gaps: empty fields, a title that was only the file
  name, missing identifiers, a missing cover. What the book has stays.
- Any other candidate waits on the **Review** page, for an editor to take
  what is right (ticked field by field), reject it, or search again.
  A rejected match is not offered again.

On a book's page, **Find details online** asks the sources there and then,
whether or not the book was looked up before, and lets you pick a
candidate and the fields to take from it.

In both, the fields worth taking are ticked to begin with, and each says
why: ticked where the book has nothing, only a guess from the file name,
or where the candidate shares an ISBN with the book (the same edition)
and no person set the field; unticked where taking it would replace your
edit or what the file says, where the candidate's title, subtitle or
description is in another language than the book, and where the field is
locked. Rows that would replace a value the book has are highlighted.
Change the ticks before taking.

## Measuring how well a lookup finds a book

A book with an ISBN is found by it; one without is found by title,
author and language alone, and the score decides which candidate is
taken. `gotome match-check` measures how often that finds the right book
on your own library: it takes a sample of books that have an ISBN
(chosen by a hash of each book's ID, so the same one each run), looks
each up as if it had none, and checks the ranking against the ISBN.

```
docker compose exec app gotome match-check -n 100
```

(`make match-check ARGS="-n 100"` for a development stack.) Options: `-n`
books in the sample (100), `-providers openlibrary,hardcover` to ask
only those (by default those `metadata.providers` names), `-json` for
the whole report. It asks the sources at their usual pace and through
the same 30-day cache as every lookup, so it first prints about how long
it will take, and a second run within the month asks almost nobody. It
changes no book. A source that is not set up (Hardcover without its
token) finds nothing for every book.

The report counts, for every source together (`all`, as a lookup ranks
them), for each source and for German, English and other books, what the
first candidate is:

- **right**: it carries one of the book's ISBNs. Open Library and
  Hardcover list works, not editions, without an ISBN, so their right
  answers mostly show as the next column.
- **same work**: another edition of the book: looked up by the book's
  ISBN, the source names the same work (Open Library's work, Hardcover's
  book). Taking it fills gaps with what that edition says.
- **edition**: the book's title and one of its authors, but neither its
  ISBN nor its work: most often another edition, as from a source that
  names no works (the DNB). Its description is the book's; its ISBN,
  cover and page count may be another edition's.
- **wrong**: another book.
- **nothing** found, or **failed** (the source did not answer; counted
  nowhere else).

Then where the book stood in the ranking when another edition or a wrong one was first, the
first candidate's score in bands for right and wrong ones, and at each
threshold how many first candidates a lookup would take and how many of
them are right: the line marked `*` is `metadata.matchThreshold` now.
Raise the threshold until wrong ones are rare among those taken; what
falls below it waits on the Review page.

Then, for the rule an automatic run uses on matches (below), how many
of the sampled books have two sources whose first candidates name one
ISBN or one work, and how many of those are right, of the same work, or
wrong. Read it before turning `curation.matches.auto` on: the share of
wrong among the accepted is what that rule would get wrong on your
library; only numbers are printed, never titles. It measures the
agreement between sources; the checks against the text's language and the
file's year need the text and are not part of it.

Last, what `curation.matches.edition` would take as well: books where no
two sources agree but one source's first candidate has the book's title,
an author of it and the file's publisher, with a year at most one off.
On the library above it took 49 of 200 books: 36 the book's own edition,
13 another edition of it, none another book. Turn it on only where your
numbers say the same.

## Learning the score from your library

The score that ranks candidates was set by hand: how much the title,
the authors, the year, the language, the publisher, the pages, the
series and its place, and the kind of edition count. A library's books
that have an ISBN are labelled examples of how well that works: look up
without the ISBN, the candidate that carries it is right. `gotome
match-train` fits a logistic regression on them, so that the score
becomes a probability: a threshold of 0.95 then takes matches that are
right 95 times in 100 on your library, which the calibration table of
`match-check` shows (what a score predicts against what was found).

```
docker compose exec app gotome match-train -n 1000
```

It looks a sample up as `match-check` does (the same sample, the same
cache, the same pace; `-providers` and `-json` too), computes for every
candidate the features the score uses (title likeness, authors in
common, year, language, publisher, pages, series and place, kind of
edition, which source, and the place in the source's answer), and fits
the weights on four fifths of the books. It prints the weights beside
the hand-set ones, and, on the fifth held out, the hand-set and the
learned score side by side: what comes first, what each threshold takes
and how much of it is right, and the calibration. The weights it keeps
are then fitted on all of the sample, and stored in the database (not in
a file), per installation. Without enough right candidates (30) or
books to hold some out (10) it keeps nothing.

The same two commands are on the settings page, under **Book details**:
**Measure** (`match-check`) and **Learn from my library** (`match-train`)
each run in the background with their progress ("312 of 1,000 books
looked up", the time left at the sources' pace), and show their last
result as tables. `metadata.scoreWeights` chooses which weights rank a
lookup: the hand-set ones (the default, so an update changes nothing),
the learned ones (without any, the starting weights shipped for the
languages of the library, and without those the hand-set ones), or an
imported file's.
`metadata.learnScore` learns once a week, where at least 500 books have
an ISBN, through the cache.

**What it found on a library in German** (1,000 books with an ISBN looked
up without it at the DNB and Open Library; 810 to fit, 190 held out; the
score ranks the held-out books' candidates):

| the first candidate | hand-set | learned |
|---|---|---|
| the book itself (its ISBN) | 76 (40%) | 86 (45%) |
| another edition of it | 46 (24%) | 36 (19%) |
| another book | 9 (5%) | 12 (6%) |
| nothing found | 55 (29%) | 55 (29%) |
| log loss of the candidates, held out | - | 0.346 |

The hand-set score is no probability: its 0.95 and above took 79 of the
190 books, of which 50 carry the ISBN and 27 are another edition (63%
and 34%), where it says 99%. The learned score is: the first candidates it
puts between 0.7 and 0.8 are the book 82% of the time, between 0.8 and
0.9 90%, as it predicts or a little better. It is less sure, since an
edition that is not the ISBN's counts against it: no candidate reaches
0.95, 0.7 takes 33 books of which 29 are the book itself, 3 another
edition and 1 another book (88%), and 0.5 takes 68, of which 54 (79%).
With the learned weights, lower `metadata.matchThreshold` to what your
own table says (the setting is yours; GOtome does not move it), and note
that matches that wait for review are kept from 0.2 on, since a
probability of the exact ISBN is low for an edition that is good enough
to look at.

**What the book is about.** Where embedding is on and a book has been
embedded, a candidate found by title also counts by its description: the
descriptions of the first three candidates of each source (long enough
to say something) are embedded like a search query and their cosine to
the book's own text vector is one more feature of the score. It is meant
to tell another book of the same author and a like title from the book
itself. Only some sources return a description with a search: Hardcover
and CrossRef do; the DNB and Open Library give one only for a lookup by
ISBN, so for books looked up there by title the feature is absent and its
learned weight stays zero. Nothing is compared where the book has no
vector, the candidate no description, or embedding is off; books looked
up by ISBN are never compared; the vectors of descriptions are kept in
memory by the hash of the text, not stored. `match-check` and
`match-train` take `-descriptions=false` to measure without it and
`-embedded` to sample only books that have been embedded. The learned
weights decide how much it counts: the hand-set score does not use it.
On a German library looked up at the DNB, Open Library and CrossRef
(about 290 embedded books) CrossRef's abstracts gave the feature to a
few candidates and it learned a weight near zero: no gain could be
measured. Hardcover, which describes most of its books, was not asked.

**Sharing what was learned, never the books.** *Export weights* writes a
small JSON file: the weights, the version of the feature set, the
version of GOtome, how many books were learned from and the shares of
languages and providers. It has no title, author, ISBN, path or ID; read
it before you pass it on. *Import weights* keeps such a file beside your
own, refused when it was made for another version of the features, and
**Measure** then ranks the sample with it too, so that you see how it
does on your library before you choose it with `metadata.scoreWeights`.
GOtome never sends anything anywhere itself. Starting weights for a kind
of library are built into a release from files that were shared; with
`learned` chosen, an installation uses the one that fits the languages
of its books until it has learned its own.

## Doing it automatically

Clean-up suggestions and matches that wait for review can be dealt with
by rule where the data decides, and only there. Every such run is a
run of the **Changes** page, started by a person or by the daily run the
settings `curation.cleanup.kinds` and `curation.matches.auto` switch on.
Its changes are not written into the files until you confirm it; each
change says which rule made it and why, and can be undone while its books
are as it left them. A **dry run** records what the rules would do, with
their reasons, and changes nothing.

Clean-up: a stand-in where an author belongs ("authors_sort",
"Unknown") is taken off; a shop's addition after a title is cut; one
person's spellings are made one only when they differ in the order
around a comma, case, accents, punctuation or initials with and without
points (the spelling most books use stays); a series written into
titles is set when at least three titles name it, no two at one place,
and a visible library has the series or a stored answer of a source names
it. An author who is the name of a series, or two people with the same
words in another order, always wait for you. Locked fields stay as they
are, and only books you may see are changed.

Review: a match is trusted when it shares an ISBN with the book or when
another source's match for the book names the same ISBN or work. Each
field is then decided as when two descriptions of a book are
reconciled: the source's value replaces one that is empty, a guess from
the file name, or the same value left untidy (a shop's addition on the
title); a value the book has stays; a real difference stays for you, and
the match stays on the Review page with only that field open. A match
whose language is not the language the text was detected in, or whose
year lies more than two years from the year the file itself gives, is
dismissed. The rest wait; the Review page groups them by source and score
and lets you look at a sample and accept the group.

## Who wins

A field has a source: the file, a metadata source, or a person.

- **A person's edit wins.** **Edit details** on a book (or a bulk edit in
  the library) locks each field it changes. A locked field is kept from
  every automatic source: no file and no lookup changes it. Unlock it to
  let them again.
- **Files** fill a field when it is empty, was guessed from a file name,
  or was last set by the same file. Of two files of one book, an EPUB
  wins over a Kindle file, which wins over a PDF.
- **Lookups** fill gaps, as above. Taking values from a candidate by hand
  replaces them, but leaves locked fields alone.

Where a library is writable, details edited in GOtome are written into
the book's EPUB files as well ([users-and-libraries.md](users-and-libraries.md)).

## Cleaning up

**Clean up** (editors) suggests fixes GOtome finds across the library: one
person under several spellings ("Brandt, H.K.L." and "H. K. L. Brandt"),
placeholders a tool left as an author ("Unknown"), an author who is
really a series, shop additions in titles ("(German Edition)"), and a
series written into the titles of books that have none ("Sternwanderer
01 - Leuchtturmwärter", "Leuchtturmwärter (Sternwanderer
#1)"). A series is suggested only where two books of a library name it
or a book is in it already; applying one puts the books into it at the
places their titles say and takes it off their titles.
Applying one is an edit like any other; dismissing one keeps it away until
what it was about changes.

## Wishlist

**Wishlist** looks a book up with the sources by title, author or ISBN,
and keeps it as a wish: a book without files. When a file of that book
arrives in the library chosen for it (by ISBN, or title and author), the
wish is fulfilled and the wisher is told.

A wish has the reading status "Wishlist". The same status on a book the
library already holds is called "Want to read": the page lists those
books apart from the wishes, under "Want to read" and "Not in the
library yet".
