# How good similar books are

`gotome similar-check` measures the similar books an installation shows, on
its own library. It reads only. In a deployment:

```sh
docker compose exec app gotome similar-check          # 300 books, 10 similar books each
docker compose exec app gotome similar-check -v       # with each book's similar books
docker compose exec app gotome similar-check -json    # to keep and compare later
```

and `make similar-check ARGS="…"` against this checkout's stack.

It asks about books with text whose author has at least five books, chosen
by a hash of their ID, so the same library gives the same sample each time,
and takes their similar books as the book page does for someone who sees
every library. It reports:

- **By the book's own author**: the share of similar books by one of its
  authors, whichever way round the name is written. A book page links to
  the author's other books already; a high share means the list shows
  little besides.
- **From the book's series**: the same, among the similar books of books
  in a series.
- **Copies of the book**: similar books with its title once a shop's
  additions are off ("(German Edition)", ": Roman") and an author in
  common. These should not be there.
- **Authors among them**: how many authors a book's similar books have, on
  average.

## Measurements

The owner's library: 49,666 books, mostly German, multilingual-e5-small,
16 passages a book.

| When | Change | Own author | Own series | Copies | Authors |
|---|---|---:|---:|---:|---:|
| 2026-10-09 | none, measured by hand in SQL (300 books) | 51% | 69% | seen | – |
| 2026-10-09 | none, `similar-check` (300 books, 68 in a series) | 55.2% | 48.5% | 0.0% | 5.0 |
| 2026-10-09 | #182: two books per author and series, copies left out | 18.7% | 14.3% | 0.0% | 8.3 |

The first `similar-check` counted as copies only titles equal once a
shop's additions were off; since #182 it also counts a title that is
another's with a subtitle ("Rauklands Sohn" and "Rauklands Sohn: Raukland
Trilogie"), which it found none of among the 300.

The caps fill a list from the nearest 20 times as many books as it shows;
at 8 times, 146 of the 3,000 places stayed empty, for books of authors
with hundreds of books. Seen in the lists: a number in a title draws
other titles with it ("CC-5 streng geheim" and "Schlachthof 5"), which is
the metadata vector's (#184).

## Searching by description

`similar-check -describe "…" -k 8` prints the books nearest a description,
as the search page finds them for someone who sees every library. Tried on
the owner's library on 2026-10-09 (#183):

- "Ein Kommissar ermittelt in Venedig": Nicolas Remin's Commissario Tron
  novels, Venedigs Mörder, Venezianische Verwicklungen, Venedig sehen und
  stehlen; one crime novel set in Bozen.
- "Familiensaga über drei Generationen auf einem Gutshof in Ostpreußen":
  East Prussian family stories, and memoirs of the flight from it.
- "Kochbuch mit vegetarischen Rezepten": eight vegetarian and vegan
  cookbooks.
- "Biografie eines berühmten Komponisten": books about musicians, a
  Mozart biography, Wagner's works; a dictionary of quotations.
- "Liebesroman in Cornwall": Cornwall romances and Du Maurier, but first
  two novels by Bernard Cornwell: the metadata vector holds names, which a
  word of the description can meet (#184).
- "a space opera with a war between galactic empires": some science
  fiction among unrelated English books; the library is mostly German.

Copies were listed twice ("Abschied und Wiedersehen"), so a page leaves out
a copy of a book before it.
