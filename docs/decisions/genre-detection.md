# Genre detection

Decided in #451, 2026-11-12. Harness: `spikes/genres`, whose `results/` hold the
tables quoted here.

## Question

Genres (#175) come out of a fresh install only from a book's tags. Without
tags, `neighbours` copies the genres of the nearest books (F1 0.54 and a genre
for 62 % of the books on the owner's library, #308), and the zero-shot vector
method is barely above guessing (F1 0.16). Which methods can give a book genres
from its whole text, in English and German, on a library with no tags at all,
which of them should ship, and what must a library teach it?

## Decision

1. **Ship one method: an ensemble that suggests, calibrated per genre to
   precision 0.8, abstains below that, and gives at most three genres.** It is a
   new value of `genres.suggest`, `ensemble`. Its inputs are a head on the book's
   mean content vector, a head on the metadata vector, TF-IDF word weights over
   the whole text, and `neighbours`. Suggestions wait in the review as today;
   nothing is accepted by itself and tag genres stay what they are.
2. **`genres.suggest` offers `off`, `tags`, `neighbours` and `ensemble`, and
   `ensemble` is the default.** `vector` (zero-shot, F1 0.16) and `combined` go:
   they are kept only for `genre-check` until removed. The default is the owner's
   to confirm: it changes `off` to a method whose measured precision on a modern
   German library is 0.6 to 0.7, not the 0.8 it is calibrated to.
3. **Trained on Gutenberg and shipped, as numbers in the binary**: a logistic
   head on the e5-small content vector for each of the 25 genres Gutenberg has
   15 books of (38.5 KB), the shared stacker weights and the thresholds, and for
   **English only** the TF-IDF table (30,000 words times 25 genres, 1.5 MB as 16-bit
   numbers, 0.75 MB as 8-bit, plus 0.3 MB of words). German gets no shipped
   table: it carries old German, and on a modern German library it reached F1
   0.25 (embeddings alone 0.65). Without tags, a German library is served by the
   embedding heads alone.
4. **Adapted per library**, as a daily job and when tags change by a fifth: heads
   and TF-IDF per language retrained on the library's tagged books and accepted
   suggestions together with the shipped training set (the shipped numbers keep
   the training counts, not the books), thresholds recalibrated out of fold on
   the library's own labels once it has at least 100 books with a genre. The
   owner's wish to keep the custom classifier as a method of its own is met by
   this retraining and measured as `library` below; it is not a different
   mechanism.
5. **Not shipped**: voting over passages (top 3, share or mean of passage
   scores), which needs a vector for every chunk, four times what a book has
   today, and was never better than the mean vector; pruned term tables (keeping
   1,000 words a genre loses a third of the F1); the metadata head as a method of
   its own (F1 0.18 to 0.29); `vector`. The generative method 7 was not tried: no
   GPU server was at hand. The providers' subjects (method 1) could not be
   measured, see Limits.
6. **What it cannot do out of the box**: Gutenberg has 15 books or more for
   25 of the 44 genres, mostly classics. Horror, thriller, contemporary,
   classics, dystopia, erotica, young adult, family saga and comics have fewer, so they
   come only from tags or from a library's own tagged books. The shipped model
   therefore covers the genres of old books; for the rest it is the adaptation
   that works.

## Measurements

Precision (P), recall (R), F1 and the share of books given a genre (given), at
the per-genre thresholds calibrated to P 0.8 on the training set scored out of
fold. The last column is the best single threshold for all genres, picked on
the test books as `genre-check` does, so it is optimistic. Folds are five, by a
hash of the book (Gutenberg) or of its folder (library).

### Gutenberg: 1,171 books with a genre, 25 genres, English, German, French

| Method | P | R | F1 | given | best single threshold F1 |
|---|---|---|---|---|---|
| TF-IDF, whole text | 0.78 | 0.53 | 0.63 | 68 % | 0.66 |
| head on the mean vector | 0.75 | 0.41 | 0.53 | 62 % | 0.62 |
| passages, top 3 | 0.80 | 0.25 | 0.38 | 38 % | 0.55 |
| passages, share above 0.5 | 0.77 | 0.34 | 0.47 | 49 % | 0.59 |
| passages, mean | 0.76 | 0.38 | 0.50 | 58 % | 0.61 |
| metadata vector (title, authors) | 0.70 | 0.18 | 0.29 | 34 % | 0.42 |
| neighbours (10 of about 940 books) | 0.82 | 0.31 | 0.45 | 45 % | 0.55 |
| ensemble, all inputs | 0.79 | 0.56 | 0.65 | 72 % | 0.69 |
| ensemble without passages (the one chosen) | 0.78 | 0.55 | 0.65 | 72 % | 0.69 |
| ensemble of embeddings only | 0.78 | 0.50 | 0.61 | 69 % | 0.66 |

In English the ensemble reaches F1 0.69 (P 0.80, R 0.60), in German 0.61 (P 0.77,
R 0.51). Gutenberg has no descriptions, so the metadata vector is title and
authors alone. Per genre (P / R, books): easy are western (0.82 / 0.98, 110),
science fiction (0.78 / 0.92, 61), drama (0.82 / 0.91, 77), travel (0.81 / 0.80,
97), crime (0.78 / 0.79, 86), poetry (0.82 / 0.75, 97), children (0.79 / 0.70,
131), psychology (0.70 / 0.81, 32); hard are romance (0.60 / 0.09, 100), war
(0.67 / 0.13), history (0.50 / 0.08), and adventure, fantasy (19 books) and
humour, which abstain. `results/gutenberg.md` has all 25.

### A library: 1,515 books of whole folders

The sample holds 1,510 German and 5 English books; 31 % have tags, 23 % (355
books) a genre from them, 30 % a description, 1.1 % no text. Eight genres have
15 books or more (crime 103, horror 70, thriller 60, romance 44, contemporary 40,
fantasy 35, science fiction 32, historical 21). Cross-validated inside the
library (311 test books, about 250 to train on):

| Method | P | R | F1 | given | best single threshold F1 |
|---|---|---|---|---|---|
| TF-IDF, whole text | 0.54 | 0.25 | 0.34 | 49 % | 0.64 |
| head on the mean vector | 0.55 | 0.22 | 0.31 | 43 % | 0.60 |
| passages, mean | 0.60 | 0.16 | 0.25 | 32 % | 0.56 |
| metadata vector (with description) | 0.68 | 0.11 | 0.18 | 19 % | 0.44 |
| neighbours (10 of about 250 books) | 0.67 | 0.06 | 0.12 | 13 % | 0.53 |
| ensemble without passages | 0.67 | 0.32 | 0.43 | 57 % | 0.64 |
| ensemble, all inputs | 0.62 | 0.31 | 0.41 | 59 % | 0.64 |

With so few books to calibrate on, most genres miss their thresholds and give
little; at one threshold chosen after the fact the same models reach F1 0.64.
That is the argument for needing 100 labelled books before recalibrating, and for
a threshold that errs low. Per genre, the ensemble: crime 0.77 / 0.35, horror
0.74 / 0.46, romance 0.73 / 0.50, historical 0.77 / 0.48, science fiction
0.69 / 0.34, fantasy 0.40 / 0.49; contemporary and thriller are not found (0 and
0.02 recall). `neighbours` here is not what runs on the whole library (48,560
books, F1 0.54): its pool is a few hundred books. `gotome genre-check` was not
run on a stack.

### What a shipped default does, and what adapting adds

Five genres are in both sets (crime, fantasy, historical, romance, science
fiction); 221 library books carry one of them. All three runs are tested on the
same books:

| Trained on | Method | P | R | F1 | given |
|---|---|---|---|---|---|
| Gutenberg only | head on the mean vector | 0.71 | 0.60 | 0.65 | 78 % |
| Gutenberg only | TF-IDF | 0.42 | 0.18 | 0.25 | 45 % |
| Gutenberg only | ensemble, without passages | 0.71 | 0.62 | 0.66 | 82 % |
| Gutenberg only | ensemble, embeddings only | 0.77 | 0.62 | 0.69 | 77 % |
| the library alone | ensemble, without passages | 0.75 | 0.71 | 0.73 | 87 % |
| Gutenberg and the library | ensemble, without passages | 0.75 | 0.77 | 0.76 | 93 % |

The embedding heads carry over from old books to modern German; the words do
not. A library's own books are worth more than the shipped ones, but the shipped
ones add to them (F1 0.73 to 0.76, with about 180 library books to train on).
These five genres are the easy part of the list, and a book counts only if its
tags gave one of them, so none of these figures are what a library with every
genre sees.

### Books without text or description

1.1 % of the library's books have no text (scanned PDFs and audiobooks would add
to that; the sample is EPUBs only). For them only the metadata vector and the
neighbours remain, and the metadata vector alone gives a genre to 17 to 19 %
of books at F1 0.17 to 0.19, with or without a description (30 % have one):
the description did not help in this sample. Such books are left to tags and
to providers. A book with text and no description loses nothing measurable: the
ensemble gives F1 0.43 for books without one and 0.40 for those with.

### Cost

Measured on a machine with a load average of 15 to 20 on 12 cores, so CPU times
are on the high side. Extrapolated to 48,560 books on 2 CPUs.

| Step | Measured | 48,560 books on 2 CPUs |
|---|---|---|
| Counting words while the text is in memory at extraction | 11 ms a book (mean 519,000 characters) | 4.6 minutes |
| Reading the files again for books already extracted (parse 33 ms, count 11 ms), as `Reindex` does | 44 ms | 18 minutes |
| Scoring: TF-IDF 0.3 to 0.5 ms, heads 0.03 ms | under 1 ms | under 1 minute |
| `neighbours` | unchanged | as today |
| Training TF-IDF on 936 books | 2.5 s on 4 threads | about 85 CPU-seconds a fit for 8,000 tagged books, 7 CPU-minutes with the four inner folds, a language each |
| Training the heads on 936 books | 0.5 s on 4 threads | seconds |
| Training the passage head on 936 books (16 vectors each) | 2.1 s on 4 threads | not shipped |
| Embedding 16 passages and the metadata text | 8.6 CPU-s a book here | the cost of #15, 40 hours on 4 CPUs there; nothing new |

Memory: a book's word counts are a transient map of about 10,000 words, under
1 MB. The English table is 3 MB in memory as 32-bit numbers, the heads 38 KB.
Training TF-IDF holds a sparse matrix of the training books (about 50 KB a
book): cap it at 4,000 books a language, 200 MB. The harness's own 2.1 GB peak is
it holding every record at once. Nothing here needs more than the 5 GB.

### Size of a shipped default model

| Part | Size |
|---|---|
| Head per embedding model, 25 genres | 38.5 KB for e5-small (384 numbers and a bias), 102.5 KB for bge-m3 |
| TF-IDF table, English, 25 genres | 1.5 MB as 16-bit, 0.75 MB as 8-bit, plus 30,000 words (0.3 MB) |
| TF-IDF table, German, 21 genres trained | 1.26 MB as 16-bit (not shipped) |
| Pruned to 1,000 weights a genre | 0.27 MB, but F1 0.47 against 0.63 |
| Stacker and thresholds | under 2 KB |

## Why these

- **Whole-text words beat vectors on Gutenberg and the vectors win where the
  language is not the training set's.** TF-IDF is the best single method on
  Gutenberg (0.63, as the literature's 0.49 on ten genres of 762 books led one
  to expect) and in its own library, but trained on old text it fails on modern
  German (0.25) where the multilingual vectors transfer (0.65). Hence words
  are learned per library, vectors shipped.
- **Voting over passages did not pay.** The issue expected the mean vector to wash
  genre out. Here the mean head is at least as good as any passage score (0.53
  against 0.38 to 0.50 on Gutenberg, 0.31 against 0.19 to 0.25 in the library) and the
  ensemble without passages is within 0.02 of the one with them either way (0.65
  and 0.65 on Gutenberg; 0.43 and 0.41 in the library; 0.66 and 0.68 shipped). The mean vector is stored for every book already.
- **The ensemble adds 0.02 on Gutenberg and 0.07 to 0.09 in a library over its
  best input, mostly recall.** Calibrating each genre to a precision is what
  keeps any method honest: at one threshold for all genres the same models give
  a genre to 75 to 99 % of books at precision 0.3 to 0.7.
- **Nothing replaces tags**: they are the labels, and where a book has them the
  genre is not suggested.

## Limits

- The labels are tag genres. A book counts as having a genre only if its tags
  gave one; every other genre of the book is counted as wrong. Gutenberg's
  headings give 61 % of its books a genre, with noise (a "Short stories" label
  on 238 books, a western series on 110).
- The library sample is 1,515 books of 99.7 % German, 355 with a genre and
  311 in the folds; per-genre figures there stand on 20 to 100 books. It is
  not the owner's whole library, `genre-check` was not run on a stack, and no
  English modern text was measured. Whether a shipped English table survives
  modern English is open.
- The providers' subjects (Open Library, Hardcover) could not be fetched. They
  are method 1 in the issue: `genres.Spellings` maps whatever tags arrive,
  and the provider code already writes subjects as tags, so they add labels
  and are not a model. Their coverage is unmeasured.
- TF-IDF here has no stemming; Tantivy's Snowball stems would shrink the table
  and may help a little. The top words per genre are explainable but contain
  names and boilerplate from Gutenberg's own headers ("etext", "transcribed");
  the shipped table must strip those before it is trained.
- bge-m3 was not measured; its vectors are larger, and the heads would be.
  Weights were not tuned (C = 1 for heads, 10 for words, 60 and 80 iterations).
- Passage voting was tested on 16 passages a book, the number of #15, not every
  chunk.

## Follow-ups to file

1. Word counts at extraction and a TF-IDF scorer and trainer in Go
   (`internal/genres`), the English table shipped, German per library.
2. Heads on the content vector, the stacker, out-of-fold calibration and the
   `ensemble` value of `genres.suggest`; review shows the reasons (the words
   that decided, the neighbours' genres).
3. The shipped training of the default (a reproducible command from
   `spikes/genres`, Gutenberg headers stripped) and its refresh when
   `Version` changes.
4. The daily adaptation job and its threshold rule for small libraries.
5. A labelled modern corpus under a free licence for the genres Gutenberg lacks.
6. `genre-check` for the ensemble, so the owner can see the figures of this record on
   the whole library.
