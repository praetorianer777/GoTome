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
