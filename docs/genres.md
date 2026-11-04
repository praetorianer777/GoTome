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
  part by part. A tag in none of the lists gives no genre and stays a tag.
  Add spellings there; `gotome genre-check` lists the tags of the most
  books that give none.
- **Suggestions.** From a book's vectors (#66): how much nearer they are
  to a genre's descriptions than to the genres on average (`vector`), or
  which genres most of its nearest books in the library have
  (`neighbours`), or the second where the first finds nothing
  (`combined`). At most three genres a book. A suggestion waits under
  Genres in the app until a person accepts or rejects it; a rejected one
  is not suggested for that book again.

Nothing is written until an administrator sets **Genres for books**
(`genres.suggest`) in the settings: `off` (the default), `tags`, `vector`,
`neighbours` or `combined`. The library's filter finds the genres of tags
and those accepted, never a suggestion that waits.

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
library's vectors, `-json` for the whole report.

The report gives the share of books with a genre from their tags (before)
and, per method, the share of the sample given one at each threshold
(after). From it, set `DefaultThreshold`, each `Genre.Threshold` and
`NeighbourShare`, choose the method, and only then switch the setting on.
