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
10 points: the next volume is not this book). A side that says nothing
about a signal changes nothing. Among equal scores, candidates in the
book's own language come first.

The weights of these signals (named constants in
`backend/internal/metadata/score.go`) are set by reasoning and await
numbers from `gotome match-check`: run it before and after on your own
library (below) and move them if the share of wrong first candidates
or the sureness at the threshold does not improve.

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
- **wrong**: another book, or an edition the source does not tie to the
  book's ISBN.
- **nothing** found, or **failed** (the source did not answer; counted
  nowhere else).

Then where the book stood in the ranking when a wrong one was first, the
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
