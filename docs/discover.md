# Discover

**Discover** (#393) offers a person books that no library they see holds
yet, from their own taste, each with a reason and one tap to put it on the
wishlist. It is off until an administrator turns it on.

## What it offers

- **Other works of authors you rated highly.** The authors of the books you
  rated 5 or 4 stars, or finished, best first. "By *Author*, whom you rated
  5 stars."
- **The next volumes and gaps of the series you read.** The volume after the
  last one you finished or are reading ("Next in *Series*"), a few after
  that, and the places among the volumes your libraries hold that none of
  them has ("Missing from *Series*").
- **Books of the genres you read most.** The genres of the books you liked
  (the ones their tags mean and those you accepted under **Genres**); the
  three most read are asked for.

Each book also says which of your books it is near ("Like *Book* and
*Book*") when GOtome has vectors of your taste ([similarity.md](similarity.md)
describes the profile): its title, authors, subjects and description are
embedded as a book's metadata is and compared with your taste. A book of a
series you are in, or by an author you rated 5 stars, comes before one that
is only near your taste.

Left out are books a library you see holds (by ISBN, or by title and an
author), books on your wishlist or in your reading history, books you said
you are not interested in, and the other editions of a book that is offered
already. A library you cannot see does not count: its books are offered to
you as to anyone, and the page never says that one holds them.

## Covers and details

A book shows the cover its source gives it. Where that source lists the
book without one (Open Library's newest works of an author are mostly
reprints with no cover), the cover of another source's record of the same
book is used, or, for a book with an ISBN, the one Open Library keeps under
it (`covers.openlibrary.org/b/isbn/...?default=false`, which answers "not
found" rather than a blank picture). A book with none keeps an empty frame of
the same size. The browser asks only GOtome for covers; the server fetches
the address a signed token names, from the source it names.

Click a book's title, or press Enter on it, to open its details at the side
(from the bottom on a phone): the cover large, the authors (a link to the
author's page where your libraries hold a book of theirs), series, year,
publisher, pages, language, ISBNs, categories, the whole description, the
source and why the book is offered, with **Add to wishlist** and **Not
interested**. Escape, the close button or a click outside closes it and puts
focus back on the title; the address (`/discover?book=...`) keeps the open
book, so the back button closes it.

## Turning it on

Under **Settings**, **Discover books beyond the libraries**
(`recommendations.discover`, default off). Until then the page says that an
administrator has not turned it on.

### What the providers see

GOtome asks the metadata sources that are enabled
([metadata.md](metadata.md)) for the works of an author, the volumes of a
series and the books under a subject, through the same requests, rate limits
and cache as following authors and series
([notifications.md](notifications.md)). What a source sees is:

- the names of the authors, series and genres asked about;
- your server's address, as with every lookup.

It never sees who asked, how a person rated anything, or what a library
holds. Your library does not leave the server. Following an author or a
series already asks the same questions; Discover reuses what following found
in the last two days instead of asking again.

With `GOTOME_OFFLINE` set nothing is asked, and the page says so.

## Using it

Open **Discover** in the sidebar and choose **Find books for me**. GOtome
works in the background: a person's run asks about up to 8 series, 8 authors
and 3 genres, a few seconds each, and runs again once a day. The page shows
the result when it is done and when it was made. **Look again** asks for a
new run.

- **Add to wishlist** puts the book on your wishlist, as **Wish for a book**
  does; it leaves this page at once, and when the book arrives in a library
  your wishlist book joins it.
- **Not interested** leaves that book out for good, and any book of the same
  title and an author in common.

## Backups

The candidates and the runs are derived and left out of backups; a restored
installation finds them again on the next run. What you said you are not
interested in is yours and stays.

## Checking the providers

The subject listing of Open Library is part of the contract tests
(`make contract-test`). The answers the tests replay are recorded with
`make record-fixtures`.
