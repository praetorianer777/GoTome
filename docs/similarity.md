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

The owner's library: 49,666 books, mostly German, 16 passages a book;
multilingual-e5-small unless the row says otherwise.

| When | Change | Own author | Own series | Copies | Authors |
|---|---|---:|---:|---:|---:|
| 2026-10-09 | none, measured by hand in SQL (300 books) | 51% | 69% | seen | – |
| 2026-10-09 | none, `similar-check` (300 books, 68 in a series) | 55.2% | 48.5% | 0.0% | 5.0 |
| 2026-10-09 | #182: two books per author and series, copies left out | 18.7% | 14.3% | 0.0% | 8.3 |
| 2026-10-11 | #173: bge-m3 on an Ollama server, passages cut at 512 tokens | 18.8% | 13.8% | 0.0% | 8.3 |

The caps hold both models to the same shares, so these figures cannot tell
them apart. Their lists differ: of the 2,958 similar books e5 gave the 300
books on 2026-10-11, bge-m3 gave 28% too. Read side by side, both are
plausible; bge-m3 more often keeps to the book's kind (to a Mrs. Harris
novel it gives light holiday novels, where e5 adds a science fiction
magazine).

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

The same descriptions on 2026-10-11 (#173), six books each, e5 against
bge-m3:

| Description | multilingual-e5-small | bge-m3 |
|---|---|---|
| a detective solves murders in Victorian London | Deaver, cat mysteries, Inspector George Gently: English crime, none Victorian | Sherlock Holmes, Rivers of London, Christie, Martha Grimes, Konsalik's Finchley |
| Raumschiff Abenteuer im fernen Weltall | space adventure pulps | the same kind: Rettungskreuzer Ikarus, Mark Brandis, Lem |
| cooking Italian pasta at home | pasta, pizza and Mediterranean cookbooks, Paltrow | pasta and Italian cookbooks, one vegetarian one |
| Liebesroman in Cornwall | two Cornwell novels first | Cornwall romances first, two Cornwell novels after |
| a space opera with a war between galactic empires | four science fiction books, beside Bobby Fischer and a fantasy novella | six about science fiction: space operas, a war in space, a guide to Star Trek novels |
| Ein Kommissar ermittelt in Venedig | Venetian crime novels | Venetian crime novels, a Venice guide |

bge-m3 finds what a description means where its words are not the
books' language, as an English description of a German library; e5 does
so poorly. A name in the metadata vector still draws Cornwell to Cornwall
with both (#184). bge-m3 is the model to use where an embedding server can
run it; e5 stays the one that needs none.
