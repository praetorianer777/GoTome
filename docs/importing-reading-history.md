# Your reading history, and importing it

GOtome keeps what you read, and when, in your account. It is yours alone:
nobody else, not even an administrator, sees your status, stars, dates or
notes.

## Books you read but the library does not hold

A book read years ago, borrowed or on paper has no file here, and still
belongs in your history. On **Wishlist**, look the book up with "Look it
up", then choose:

- **Add as read**, with your stars and the day you finished it. The book
  counts in that year on **Your reading**.
- **Currently reading**, or the existing "Wish for it".

Such a book is a placeholder: it is not in the library's lists, search or
facets, and takes no part in duplicate detection or similar books. It goes
into the library that holds most of the series or the authors, otherwise
the library you chose before, otherwise the only one you see; with several
libraries and nothing to decide, choose one in the list and GOtome
remembers it. When a file of the book arrives, it is added to the
placeholder, and your status, stars and dates stay.

Every book page has a **Private note** for a review or anything else you
want to remember. Nobody else sees it.

## Your reading

**Your reading** (in the navigation) lists everything you read, are
reading or gave up, books the library holds and those it does not. Narrow
it by year, status or stars, or search the titles and authors.

## Importing from Goodreads or StoryGraph

On your profile, **Import reading history** takes the CSV file of your
export.

- **Goodreads**: My Books, Import and export, Export Library.
- **StoryGraph**: Manage Account, Export StoryGraph Library.

GOtome reads the file in memory and does not keep it. First you see a
preview and nothing is written:

- how many books are already in a library you see (found by ISBN, else by
  title and an author),
- how many it does not hold, which become books you read but do not own,
- the rows that could be several books, for you to pick the right one or
  leave out,
- the rows with nothing to import, such as a shelf GOtome does not know.

Books the library does not hold are looked up afterwards, by their ISBN
or else their title and authors, as a file read is: the cover, description
and what the file lacked are filled in in the background. This needs a
metadata provider and "Look new books up" (`metadata.autoMatch`) on; without
a connection to one (`GOTOME_OFFLINE`) nothing is asked and these books keep
what the file said.

StoryGraph rates in fractions of a star; GOtome rounds them to whole stars
and says how many it rounded.

**Import** then sets the books one at a time in the background:

| In the file | In GOtome |
|---|---|
| read, currently-reading, to-read, did-not-finish | completed, reading, want to read, abandoned |
| rating | stars (none for 0) |
| date read | the day you finished it, counted in its year |
| custom shelves (Goodreads) and tags (StoryGraph) | collections of the same names |
| review | your private note |

What you already set stays: a status other than unread or want to read, a
rating, a finish date or a note is not replaced unless you tick "Replace
what I already set". Importing the same file again changes nothing. A book
read more than once with a single date counts once.
