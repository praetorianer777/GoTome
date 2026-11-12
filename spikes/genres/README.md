# Genre detection spike (#451)

The harness behind `docs/decisions/genre-detection.md`. It is its own Go
module (`replace` to `backend`, like `spikes/search`, so it may import the
app's `internal/` packages: the EPUB reader, `embed.Onnx`, `genres.FromTags`)
and is not part of the gate.

## What it measures

Eight outputs of five trainers, and four ensembles of them, each trained
per genre as a one-vs-rest logistic regression (L-BFGS, own code in `lbfgs.go`
and `lr.go`):

| Name | What |
|---|---|
| `tfidf` | TF-IDF over the whole text (lower-cased words of 3 to 25 letters, no stemming, 30,000 words per language by document frequency, sublinear tf), one model per language |
| `tfidf1000` | the same keeping only the 1,000 heaviest weights per genre (size of a shipped table) |
| `mean` | head on a book's mean passage vector (16 passages, `passage: `, e5-small int8) |
| `passages-topk`, `-share`, `-mean` | one head trained on every passage, applied to each passage; combined as the mean of the best 3, the share of passages above 0.5, or the mean |
| `meta` | head on the vector of title, authors and description (`query: `); never the tags |
| `neighbours` | share of the 10 nearest labelled books (cosine of mean vectors) that have the genre |
| `ens-*` | a logistic regression over those scores (one for all genres, with the genre's prior), `ens-all` (all six inputs), `ens-text`, `ens-embed`, `ens-light`, `ens-nopassages` |

Every method is turned into decisions the same way: per genre, the lowest
score from which the books of the training set (scored out of fold, four inner
folds) reach precision 0.8 with at least five of them, else the genre
abstains; at most three genres a book. The tables also give the best single
threshold for all genres, which is chosen on the test books and so is
optimistic (it is what `gotome genre-check` reports).

Precision, recall and F1 are micro-averaged over book and genre pairs;
"given" is the share of books given any genre; macro F1 averages the genres
with at least five books. Books count only if their tags give one of the
genres measured (a genre needs 15 books in the set), and a genre a book lacks
is "absent", though a library may simply not have tagged it.

## Data

- **Gutenberg**: the 2,011 EPUBs of the search spike's corpus (`.cache/corpus`),
  in English, German and French; the labels are their Library of Congress
  subject headings, mapped by `genres.FromTags` and the rules in `labels.go`.
  1,171 books carry one of the 25 genres with at least 15 books. Folds are five,
  chosen by a hash of the book (`foldOf`); every book is tested once, by models
  that did not see it. Its text is the chunk file of #14, its passage vectors
  those of #15 (int8, 16 per book), made by `gutenberg`.
- **A library**, read only (`library`): whole folders in the order of a hash of
  their names until 1,500 EPUBs. Folds go by folder, so an author's books are
  never on both sides. The harness keeps no title, author or path; the records
  hold word counts, vectors and the genres the tags give. `results/library*.json`
  have figures only.

## Run

```
make embed-assets search-spike-extract   # what #14 and #15 left in .cache (the owner runs them; set EMBED_CACHE for a worktree)
spikes/genres/run.sh gutenberg         # records of the corpus
spikes/genres/run.sh eval              # results/gutenberg.{json,md}
spikes/genres/run.sh shipped           # results/shipped.json: size and top words of a default model
spikes/genres/run.sh library DIR 1500  # records of a library
LIBRARY=.cache/genres/library.gob spikes/genres/run.sh eval
```

`eval` with a library also runs `library` (cross-validated inside the library,
every genre it has 15 books of), `library5` (the same on the five genres
both sets have), `transfer` (trained on Gutenberg only, tested on the
library) and `adapted` (Gutenberg plus the library's other folds). The
model is opened offline: the harness fails rather than downloads. Costs are
the harness's own wall times on a loaded 12-core machine; see the record.

`results/` holds what the record quotes.
