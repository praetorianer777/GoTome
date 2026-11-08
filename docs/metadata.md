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

To get a Hardcover token: sign in at hardcover.app, open the account
settings, **API**, and copy the token. Paste it into **Hardcover API
token** on GOtome's Settings page. Like every secret it is stored
encrypted and not shown again.

GOtome asks each source at a polite pace (Open Library every 400 ms,
CrossRef every 350 ms, Hardcover about once a second, as each asks), and
keeps each answer for 30 days, so a lookup repeated soon asks nobody.
Keys are never part of what is kept.

`GOTOME_OFFLINE=true` asks no source at all. `metadata.providers` names
which are asked; leave one out to stop it.

## What happens to a new book

With `metadata.autoMatch` on (the default), every book is looked up once
its file is read: by its ISBNs and its DOI, and by title and author where
those find nothing. Each candidate gets a score: 1 for a shared
identifier, at most 0.99 for title, author and year alone.

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
person under several spellings ("Tolkien, J.R.R." and "J. R. R. Tolkien"),
placeholders a tool left as an author ("Unknown"), an author who is
really a series, shop additions in titles ("(German Edition)"), and a
series written into the titles of books that have none ("Rain Wild
Chronicles 01 - Drachenhüter", "Drachenhüter (Rain Wild Chronicles
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
