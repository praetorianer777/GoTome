# Passage vectors

Measured and decided in #174, 2026-11-04.

## Question

A book's content vector is the mean of 16 of its passages
([embedding-runtime.md](embedding-runtime.md)). A mean blurs a book that is
two things at once and cannot answer for a scene: "a book where someone
keeps a lighthouse in winter". A vector per passage could, but on the
owner's installation (47,730 books, 3.19 million chunks) that is millions
of vectors, on a server with 5 GB of memory and no GPU, a database the
owner has just brought down from 28 GB (#265), and a ParadeDB image that
has pgvector and nothing else (no pgvectorscale, no VectorChord). Before
building anything:

- Do passage vectors find books the book vectors do not?
- Where do they live: Postgres (`vector`, `halfvec`, `bit`, an int8 copy
  in `bytea`) or a file under `/data`, derived and out of backups as the
  full-text index is?
- How many bytes per passage and per 50,000 books?
- How long does a search take, with and without an index?
- How does visibility stay inside the search?

## Decision

**Build it, off by default, behind the setting `embedding.passages`**,
with the vectors in a store of GOtome's own under `/data/passages`, not
in Postgres:

- **A vector per chunk**, of its first 512 tokens (the chunks of #53, about
  8,000 characters; the model reads a quarter of each), made with the
  model in use and the prefix `passage: ` for e5, as its authors ask for a
  passage a query looks for. 3.34 million for 50,000 books.
- **Stored as int8 and as signs around a centre**: per passage the chunk,
  its library, 384 bits (where each dimension is above the mean of the
  first 2,000 passages stored), and the vector as 384 bytes with a scale.
  **446 bytes a passage, 1.5 GB per 50,000 books**, on the data volume.
  The database gains one row per file (`passage_files`: which model, which
  chunking).
- **A search reads every passage's signs** (185 MB at 3.19 million),
  keeps the best fiftieth by bits in common among the visible libraries,
  reads those passages' int8 vectors (25 MB, by their place), and takes
  the best 1,000 by cosine. **150 ms at 3.19 million passages**, no index,
  nothing held in memory between searches.
- **Visibility inside the search**: each record names its library; the
  scan skips those of libraries `visible_library_ids` does not give the
  viewer, so the best passages are taken among the visible ones only.
  What it finds is then checked in SQL against `book_chunks`, `books` and
  `passage_files`, again through `visible_library_ids`: the store may be
  behind the database (a chunk deleted, a book moved), and the database
  decides.
- **Blended into `GET /search/similar`**: a book with a passage near the
  description is ranked by 0.6 times its best passage's cosine and 0.4
  times its metadata vector's, in place of its content vector's; books
  without are ranked as before. No new mode, no new route.

The owner's server is better off with it **off**: it is days of the CPU
on four cores (below), there is no GPU, and the embedding server (#173)
is gone. Switching it on costs nothing until a pass runs; the disk it
then takes is about what the full-text index took off the database (#265
saved 17 GB; this is 1.4 GB).

## Measurements

### Does a passage find more than a book's mean?

The corpus of #15: 2,011 public-domain books in English, German and
French, 70,700 passages (every chunk of most books, 64 spread over the
longer ones), embedded with multilingual-e5-small, fp32 weights, prefix
`passage: ` (`.cache/embed/vectors-direct-fp32-64-passage.gob`, made for
#15). Queries were embedded with `query: ` (`make embed-spike`, step
`probes`):

- **18 descriptions** of a subject ("stories of cowboys, ranches and
  gunfights in the American West", "a ghost story that frightens", "poems")
  with the subject headings of the books about it; precision of the ten
  best books, and mean average precision over all.
- **1,562 sentences** of 781 books, one from the start of a chunk, which
  the model read, and one from 4,500 characters into it, past what 512
  tokens hold: where the book the sentence is from stands (MRR, the share
  first, the share among the ten best).

| Books ranked by | Descr. P@10 | Descr. MAP | Read: MRR | @1 | @10 | Not read: MRR | @1 | @10 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| book vector (mean of 16), as now | 0.183 | 0.112 | 0.195 | 0.13 | 0.31 | 0.144 | 0.09 | 0.24 |
| best passage, fp32 | 0.222 | 0.173 | 0.437 | 0.37 | 0.57 | 0.179 | 0.12 | 0.30 |
| best passage, halfvec | 0.222 | 0.173 | 0.437 | 0.37 | 0.57 | 0.179 | 0.12 | 0.30 |
| best passage, int8 | 0.233 | 0.174 | 0.439 | 0.37 | 0.57 | 0.179 | 0.12 | 0.30 |
| best passage, signs alone | 0.039 | 0.030 | 0.038 | 0.02 | 0.08 | 0.023 | 0.01 | 0.04 |
| best passage, signs, best 2,000 rescored by int8 | 0.189 | 0.109 | 0.303 | 0.26 | 0.38 | 0.133 | 0.08 | 0.22 |
| best passage, centred signs alone | 0.178 | 0.111 | 0.258 | 0.20 | 0.39 | 0.107 | 0.06 | 0.19 |
| **best passage, centred signs, best 2,000 rescored by int8** | **0.233** | **0.170** | **0.431** | **0.37** | **0.56** | **0.177** | **0.12** | **0.29** |
| mean of the best 3 passages, fp32 | 0.233 | 0.155 | 0.353 | 0.29 | 0.47 | 0.203 | 0.15 | 0.30 |
| 0.5 book vector + 0.5 best passage | 0.222 | 0.149 | 0.385 | 0.32 | 0.52 | 0.195 | 0.13 | 0.30 |

Passages find more: a description's books a fifth more often among the
ten best and half again by MAP, and a scene's book more than twice as
often first (0.37 against 0.13). For a sentence the model did not read
(past the chunk's first 512 tokens), the gain is small (0.179 against
0.144): one vector per chunk sees a quarter of the text. Covering it all
would take a vector per 512 tokens, about 10 million for the owner's
library, three times the disk and the time below.

Of the best 100 passages for each query by fp32, the other codings keep:

| Coding | Recall of the best 100 |
|---|---:|
| halfvec (2 bytes a dimension) | 0.999 |
| int8 with a scale (1 byte) | 0.967 |
| signs (1 bit) | 0.037 |
| signs, best 2,000 rescored by int8 | 0.276 |
| signs around the mean | 0.236 |
| signs around the mean, best 2,000 rescored by int8 | 0.775 |

e5's vectors share a large common part (its authors note that its cosines
lie between about 0.7 and 1 even for unrelated texts), so the plain signs of two passages mostly agree whatever they
say, and are useless (0.037). Around the mean of the passages they tell
passages apart well enough to pick which to read in int8. Results:
`spikes/embed/results/passages.json`, `passages-centred.json`.

### What the coarse pass loses as the library grows

The centred signs pick candidates from all passages; the larger the store,
the more the true best can be passed over. Measured on passages made from
the corpus's real ones (each two of them mixed and normalised, so as dense
as a library of more books on the same subjects), with 100 of the real
descriptions and sentences as queries, against the exact int8 order, 66
passages a book (`passage-recall`, `results/passage-recall.json`):

| Passages | Rescored | Recall of the best 100 passages | Of the ten best books |
|---:|---:|---:|---:|
| 100,000 | 2,000 / 5,000 / 10,000 / 20,000 | 0.73 / 0.88 / 0.95 / 0.99 | 0.89 / 0.97 / 0.99 / 1.00 |
| 1,000,000 | 2,000 / 5,000 / 10,000 / 20,000 | 0.50 / 0.68 / 0.80 / 0.89 | 0.73 / 0.84 / 0.92 / 0.98 |
| 3,190,000 | 2,000 / 5,000 / 10,000 / 20,000 | 0.44 / 0.60 / 0.71 / 0.82 | 0.59 / 0.73 / 0.85 / 0.92 |

So the search rescores a fiftieth of the store, at least 10,000:
63,800 at 3.19 million, where 20,000 already find 92 % of the ten best
books.

### Size

Postgres in the pinned image under #14's limits (2 GB, four CPUs, 512 MB
of shared buffers), loaded from the corpus's vectors mixed as above
(`spikes/embed/passages-pg.sh`, `passages.sql`); the chunk ID there is a
uuid, so a real table's rows are 8 bytes smaller (`book_chunks.id` is a
bigint). Per passage:

| Where, as what | Row | Primary key | Per passage | Per 50,000 books (3.34 M passages) |
|---|---:|---:|---:|---:|
| Postgres `vector(384)` (fp32, as `book_vectors`) | 1,638 B | ~40 B | 1,678 B | 5.6 GB |
| Postgres `halfvec(384)` | 910 B | ~40 B | 950 B | 3.2 GB, plus an HNSW index |
| Postgres `bit(384)` table and `bytea` int8 table | 117 + 455 B | ~80 B | 650 B | 2.2 GB |
| Postgres `bit(384)` and int8 in one table | 512 B | ~40 B | 552 B | 1.8 GB |
| **File store: signs record and vector record** | **58 + 388 B** | none | **446 B** | **1.5 GB** |

A vector per 512 tokens instead of per chunk (about 10 million passages)
triples every figure: 4.5 GB in the store, 9.5 GB as `halfvec`.

### Time

Searches, median of 20 queries in milliseconds, half the libraries
visible; the host was loaded by other work throughout:

| Search | 1 M passages | 3.19 M passages | 10 M passages |
|---|---:|---:|---:|
| Postgres, `vector` exact (cached) | 1,469 | | |
| Postgres, `halfvec` exact (cached) | 224 | ~700, if 3 GB stayed cached | |
| Postgres, `bit` exact, best 2,000 | | 506 | |
| Postgres, `bit` in the int8 table, best 2,000 | | 1,968 | |
| Postgres, 2,000 int8 rows by key | | 78 | |
| Go, int8 exact, four threads | 170 | 1,118 | |
| Go, signs exact, four threads | 49 | 150 | 592 |
| **The store, signs then 63,800 int8 read** (`BenchmarkSearch`) | | **150** | |

An HNSW index on `halfvec` (pgvector 0.8.4, `m` 16, `ef_construction`
64) over 1 million passages outgrew 1 GB of `maintenance_work_mem` after
737,586 of them and had loaded 92 % of the table after 67 minutes on four
CPUs, when the run was stopped; at
3.19 million its graph needs several GB to build in memory, more than the
owner's server has, and the index itself is about as large again as the
table. An exact scan needs none of it.

Embedding: e5-small with int8 weights makes 5.6–5.8 passages a second on
four cores of a Ryzen 5 3600 (#15). The owner's 3.19 million chunks are
**6.5 days** of those four cores, and every file is read again for its
text; a vector per 512 tokens would be three weeks. A NAS or an LXC on
older cores is slower by a factor not measured here.

## Reasons

- **Not Postgres.** `halfvec` is 3.2 GB per 50,000 books, an exact scan
  of it needs that in memory to be quick, and an HNSW index needs more
  memory to build than the server has, and as much disk again. pgvector has
  no int8 type, so the compact form in Postgres is `bit` for scanning and
  `bytea` for rescoring in Go: 2.2 GB, and the scan of `bit` is three times
  slower in Postgres (506 ms) than in Go (150 ms). The database would grow
  by more than half of what #265 left it. The vectors are derived, like
  the full-text index, which moved out of Postgres for the same reasons.
- **Not an ANN index.** Nothing in the image builds one that fits; the
  signs make an exact scan of 3 million passages as fast as a search
  needs, with no index to build, merge or keep in memory.
- **int8 for the score, centred signs to choose.** int8 loses nothing that
  shows (recall 0.967; the books found are fp32's); signs alone are 48
  bytes a passage but useless for e5 unless taken around the mean, and even
  then only as the coarse pass.
- **Visibility as for the full-text index**: the libraries in the scan,
  so that the best passages are the visible ones' and not cut away by a
  check afterwards, and the check against the database after, so that the
  store being behind can only lose a hit, never show one.
- **Blended, not a mode.** Passages beat the book vector for both kinds of
  query, and the metadata vector says what passages cannot (title,
  author, series); a person searching by description should not have to
  know which vectors exist.
- **Off by default.** The disk is affordable; the CPU on a server without
  a GPU is not, unless its owner decides it is.

## How it works

- `internal/passages`: `Store` (two files of fixed records and
  `meta.json` with the model, the centre and the libraries; a record half
  written when the process stopped is cut off at open), `Service.Embed`
  (the job `passages.embed` on the `embed` queue, after or before the
  book vectors' pass, never beside it; snoozing every five minutes,
  queued by chunking, by an `embedding.*` setting and hourly), and
  `Service.Near` for the search.
- A file is embedded again when its `passage_files` row is missing, of
  another model or of another `chunked_at`. Another model empties the
  store and every row. The old records of a file embedded again stay
  until the store is compacted: once a fifth of it is dead, or daily,
  it is written again with only the chunks `passage_files` names, under
  the library each is in now.
- Beginning and compacting write the two files of a new generation
  (`signs.<n>`, `vectors.<n>`) and switch to them by replacing
  `meta.json`, which names the generation (#323): a crash leaves the old
  set or the new one. A store whose `meta.json` names files that are not
  there, or one of before generations cut off between its renames, is
  started over at open, and every file is embedded again.
- Backups leave out `passage_files` (`derived` in `deploy/backup.sh`)
  and `/data/passages`; a restore reads every text again, which, with
  passages on, embeds them again.
- `GET /embedding/status` gives `passages` (books with text, those with
  their passages' vectors, the store's bytes) while they are on; the jobs
  page shows it.

## Limits

- **The scale figures are of mixed vectors**, not of a library of
  millions of passages: the quality was measured on 70,700 real ones, the
  coarse pass's loss and the times on mixtures of them. A first run on the
  owner's library should check that a description finds what it found on
  this corpus.
- **The centre is the mean of the first 2,000 passages stored**, of the
  books embedded first, not of the whole library. The corpus's own mean
  was used in the measurements.
- **The corpus is mostly English**; the owner's library is mostly German.
  #15 found e5's quality the same across the three languages.
- **fp32 weights in the measurements, int8 in the app**: #15 found their
  vectors equally good (cosine 0.98 to the reference at worst).
- **A quarter of each chunk is read.** A scene past a chunk's first 512
  tokens is found little better than by the book vector.
- **Similar books from passages**, the issue's third idea, is not built:
  books sharing several close passages would need a search per passage
  of the book, which is a pass of its own.
