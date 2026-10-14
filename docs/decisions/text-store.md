# Where the chunks' text is kept

Measured in #244, 2026-10-14. Decided by the owner in #265, the same day:
option 5, the index without the text.

## Question

On the owner's installation (47,730 EPUBs, 3.19 million chunks)
`book_chunks` takes 25 GB: 17 GB of TOAST, which is the chunks' text
compressed with pglz, and 7.5 GB of the BM25 index `book_chunks_bm25`.
Compression by the file system is not available there. Can the database
hold the same text and search in much less, and what would it cost?

pg_search builds its index from the table's rows, and `pdb.snippet` reads
the text from them, so as long as search runs in Postgres, the plain text
stays in the table. The options:

1. **Keep pg_search as it is** (pglz TOAST).
2. **lz4 TOAST compression** (`ALTER TABLE … SET COMPRESSION lz4`). Postgres
   18 has pglz and lz4 for TOAST; zstd is not among them.
3. **Text in a zstd store of GOtome's own, the index elsewhere**: per chunk,
   per chunk with a trained dictionary, per book, or in blocks; the store
   could be files on the data volume or `bytea` in Postgres.
4. **Tantivy embedded in the app** (cgo, as ONNX Runtime is; two containers
   stay): the engine under pg_search, with the text in its own zstd document
   store, on the data volume.
5. **Tantivy embedded in the app, without the text**: the index alone, on
   the data volume, and the text nowhere but in the books' files. Snippets
   and "Read from here" read the passage from the file when asked for.

## Decision

**Option 5.** Full-text search moves to Tantivy in the app's process, and
GOtome keeps no copy of a book's text: not in Postgres, not in a store of
its own. A chunk row keeps what locates its text in the file (the EPUB
entry and the range in its text, or a PDF's pages), and a snippet is read
from there on demand, one zip entry at a time. On the owner's installation
that is 7.6 GB of index on the data volume and about 3.5 GB of database
for everything else: **about 11 GB in place of 28 GB**, with full-text
search, phrases, stemming, typo repair and visibility as they are. The work
is #265, in the order its design lists: the engine and its binding, the
index written beside pg_search through an outbox, search on Tantivy behind
a setting and compared with pg_search, then the text and the BM25 index
dropped.

What this costs over option 4: a snippet opens the file (a zip entry of an
EPUB, pages of a PDF) instead of a store, which is a few milliseconds per
book shown and nothing when the file is gone (the hit is shown without
one); and everything that read the text from the table (embeddings,
re-signing for duplicates, the rebuild) reads the file again. Reading files
again is what a restore does already.

## Recommendation (before the decision)

**Keep pg_search with pglz (option 1), and do not switch to lz4.** If the
owner decides that 9 GB of disk are worth it, the way to them is option 4
with the store at zstd level 19 in 128 KB blocks, as its own milestone; the
work it takes is listed below.

## Measurements

A sample of the owner's library: every 24th of the 48,560 EPUBs in name
order, 2,024 files, of which 1,982 have text. They were read with the app's
EPUB reader and cut with `textproc.Split`, as extraction does: 131,887
chunks, 1,044.7 MB of text (66.5 chunks a book, as on the installation).
Everything was measured on these same chunks. The sample scales to the
installation by its chunks, 3.19 million over 131,887 (×24.2); it predicts
17.4 GB of TOAST and 7.7 GB of BM25 index where the installation has 17 and
7.5, so the sample is representative.

Postgres ran in the pinned image (`paradedb/paradedb:0.25.11-pg18`) under
#14's limits: 2 GB, four CPUs, 512 MB of shared buffers. `book_chunks` and
its indexes are those of migrations 00021 and 00022, loaded with `COPY` and
indexed after. Tantivy is 0.22.1 (pg_search builds on a fork of a newer
one), with pg_search's tokenizers, positions for phrases, four indexing
threads, merged to one segment.

### Size

| Where the text is | Sample | At 3.19 M chunks | Of the text |
|---|---:|---:|---:|
| Text, uncompressed | 1,045 MB | 25.3 GB | 1.00 |
| **1. pglz TOAST (now)** | 719 MB | **17.4 GB** | 0.69 |
| 2. lz4 TOAST | 804 MB | 19.5 GB | 0.77 |
| 3. zstd per chunk, level 3 / 19 | 500 / 473 MB | 12.1 / 11.4 GB | 0.48 / 0.45 |
| 3. zstd per chunk, 112 KB dictionary, level 3 / 19 | 411 / 357 MB | 9.9 / 8.6 GB | 0.39 / 0.34 |
| 3. zstd per chunk, 1 MB dictionary, level 3 / 19 | 402 / 324 MB | 9.7 / 7.8 GB | 0.39 / 0.31 |
| 3. zstd per book, level 3 / 19 | 375 / 318 MB | 9.1 / 7.7 GB | 0.36 / 0.30 |
| 3. zstd in 16 KB blocks, level 3 / 19 | 459 / 426 MB | 11.1 / 10.3 GB | 0.44 / 0.41 |
| 3. zstd in 128 KB blocks, level 3 / 19 | 414 / 363 MB | 10.0 / 8.8 GB | 0.40 / 0.35 |
| 4. Tantivy store, lz4 (its default) | 706 MB | 17.1 GB | 0.68 |
| 4. Tantivy store, zstd 3 in 16 KB blocks | 466 MB | 11.3 GB | 0.45 |
| 4. Tantivy store, zstd 19 in 128 KB blocks | 366 MB | 8.9 GB | 0.35 |

The dictionaries were trained on a tenth of the books and give the same
ratio on the other nine tenths (0.311 against 0.310), so they generalise.
TOAST takes 13 % more than the compressed values themselves (634 MB of
pglz values in 719 MB): each value is cut into rows of about 2 KB with their
headers, and the TOAST table has an index.

Text and index together:

| Option | Text | Index | Other | At 3.19 M chunks |
|---|---:|---:|---:|---:|
| 1. pg_search, pglz (now) | 17.4 GB | 7.7 GB | 0.7 GB | **25.9 GB** |
| 2. pg_search, lz4 | 19.5 GB | 7.7 GB | 0.7 GB | 27.9 GB |
| 3. own store, per book, zstd 19 + Tantivy index | 7.7 GB | 7.6 GB | 0.7 GB | 16.0 GB |
| 4. Tantivy, zstd 3 in 16 KB blocks | 11.3 GB | 7.6 GB | 0.7 GB | 19.6 GB |
| 4. Tantivy, zstd 19 in 128 KB blocks | 8.9 GB | 7.6 GB | 0.7 GB | **17.2 GB** |
| 5. Tantivy, no text | 0 | 7.6 GB | 0.7 GB | **8.3 GB** |

"Other" is the table without its text (0.5 GB) and its B-tree indexes
(0.2 GB), which stay in Postgres in every option. Tantivy's index without a
store is 315 MB on the sample, pg_search's 320 MB: the same engine, the same
size. Of it, 210 MB are positions, which phrase search needs.

### Time

Building, on the sample and scaled (minutes at 3.19 M chunks):

| | Sample | At 3.19 M chunks |
|---|---:|---:|
| pg_search, pglz: load, index | 40 s + 60 s | 40 min |
| pg_search, lz4: load, index | 44 s + 66 s | 44 min |
| Tantivy, no store (option 5) | 30 s | 12 min |
| Tantivy, lz4 store | 42 s | 17 min |
| Tantivy, zstd 3 in 16 KB blocks | 86 s | 35 min |
| Tantivy, zstd 19 in 128 KB blocks | 102 s | 41 min |

zstd at level 19 compresses about 1.5 to 3 MB/s per core (level 3 about
85 MB/s), so a store at level 19 costs three to five CPU hours for the
whole installation's text. Reading one chunk back: 46 µs from a chunk stored
alone, 55 µs with a dictionary, 81 µs from a 16 KB block, 322 µs from a
128 KB block, 1 ms from a whole book (p50, level 19).

Searches, p50 in milliseconds over five runs after a warm-up, both libraries
unless "one library"; the top 1,000 chunks as the app takes them, and with
snippets the snippets of the best 20:

| Query | pg_search, pglz | pg_search, lz4 | Tantivy, zstd 19 / 128 KB |
|---|---:|---:|---:|
| words "nacht stadt" (1,000 hits) | 8.5 | 6.4 | 2.2 |
| words "liebe krieg" (1,000) | 5.9 | 6.1 | 2.1 |
| words "magie drache schwert" (127) | 3.6 | 3.3 | 0.5 |
| words "detective murder" (8) | 2.7 | 2.5 | 0.1 |
| phrase "in der nacht" (1,000) | 16.1 | 14.9 | 9.9 |
| phrase "zum ersten mal" (1,000) | 14.5 | 14.5 | 10.6 |
| phrase "the old man" (16) | 3.8 | 3.3 | 0.2 |
| one library, "nacht stadt" | 4.7 | 4.5 | 1.3 |
| one library, phrase "in der nacht" | 11.0 | 10.3 | 8.3 |
| snippets, "nacht stadt" | 22.6 | 22.2 | 12.0 |
| snippets, "detective murder" | 10.7 | 8.9 | 4.2 |
| snippets, phrase "in der nacht" | 35.0 | 31.9 | 19.9 |

Tantivy in the process answers a few milliseconds sooner, as it has no
round trip, planner or heap; the store's compression makes no difference
to the searches (the other stores are in `results/`). All of them are well
under what a person notices. The host was loaded by other work throughout
(load average 17 to 40 on twelve cores), so the times compare the options
with each other and are not a dedicated server's.

How to run it again is in
[`spikes/text-store/README.md`](../../spikes/text-store/README.md); the
numbers are in `spikes/text-store/results/`.

## What each option costs

**1. Keep pg_search.** Nothing to build. The text and the index are in the
transaction that writes the chunks, a merge, a trash or a library move
changes them with the rows, visibility is `visible_library_ids` in the same
query, typo repair reads `search_words` beside it, `pdb.snippet` makes the
snippets, and `REINDEX INDEX CONCURRENTLY` rebuilds from the table (#57).
Backups leave the chunks out already (`derived` in `deploy/backup.sh`), so
the 17 GB cost disk on the server, not in backups.

**2. lz4.** One `ALTER TABLE` and a rewrite of the text. It is faster to
compress, but it takes **12 % more**, 2.1 GB on the installation: lz4 trades
ratio for speed, and on prose pglz compresses better. It would also only
apply to text written after the change. Not worth doing.

**3. A zstd store of GOtome's own.** It halves the text (7.7 to 9.9 GB,
depending on the layout), but only together with an index outside Postgres:
pg_search indexes columns of the table, and a column holding compressed
bytes gives it nothing to index (the image has no SQL function or
extension that decompresses zstd, so not even an expression index could
read it). So it
is option 4 with a second store of GOtome's own beside Tantivy's index. Its
best layouts:

- *Per chunk with a dictionary* (7.8 to 9.9 GB): every chunk is read alone,
  in 55 µs; the dictionary is a versioned file the store must keep for as
  long as text compressed with it exists. Training took 6 s on 4,700 chunks.
- *Per book* (7.7 GB at level 19): the best ratio, but a snippet or an
  embedding passage reads the whole book (1 ms), and a book is rewritten
  whole when it is chunked again, which is how chunks are written anyway.

As `bytea` in Postgres (with `STORAGE EXTERNAL`, so pglz does not try again)
it would stay in the database and in its transactions; as files it would
not. Either way it saves about 1 GB over Tantivy's own store at level 19,
for a second store to keep in step.

**4. Tantivy embedded in the app.** 17.2 GB in place of 25.9 GB (−8.7 GB,
a third), and Postgres shrinks by 25 GB, as the text and the index move to
the data volume. What it takes:

- *Building it in.* A Rust library behind a C interface, linked into the Go
  binary with cgo, built in the Dockerfile with a Rust stage and checked in
  the toolchain image; or an existing Go binding of Tantivy (also cgo). The
  binary is built with cgo already (ONNX Runtime); arm64 needs its own
  build. None of it is in the caches here: the crates of the binding, a
  Rust toolchain image, and, to match pg_search's tokenizers exactly, the
  `tantivy-stemmers` crate pg_search uses would have to be fetched.
- *Index updates outside the transaction.* Today the chunks and their index
  entries commit together. With Tantivy, a chunk write, a merge, a trash, a
  purge, a restore and a book moving library would each have to reach the
  index after the commit: through an outbox table written in the
  transaction and a job that applies it to the index and commits it there,
  safe to repeat. A crash in between leaves the index behind until the job
  runs; a search must not show what the database has dropped, so hits are
  checked against the table (they are already: the outer query of
  `internal/search` joins `books`).
- *Visibility in the index.* `library_id` and `book_id` are fields of the
  index, as in pg_search, and the visible libraries are a term set in the
  query, so the best chunks are still taken among the visible ones. The
  rule stays in SQL: the app asks `visible_library_ids` first and passes the
  IDs. A book moving library changes its chunks' field, which is a delete
  and re-add of each in Tantivy. The filter's book set (`Select`) works the
  same way.
- *Typo repair* stays as it is: `search_words` is a table of its own, filled
  when chunks are written, and repair only rewrites the query before the
  search.
- *Snippets* come from Tantivy's `SnippetGenerator` over the stored text
  (included in the times above), in place of `pdb.snippet`; the control
  characters around matches are the app's to add, as now.
- *Rebuild.* A new Tantivy version with a new index format means
  reindexing. With the text in its store, the rebuild reads the store
  (if the new version reads the old store) or, otherwise, every file again,
  as a restore does now. Either runs beside the old index and switches over
  when done, as `REINDEX CONCURRENTLY` does now; this has to be written.
- *Backup.* Unchanged in effect: the chunks are derived and not backed up
  now, and the index directory would not be either; a restore reads every
  text again (`chunked_at` cleared), which then fills the index.
- *Other readers of the text.* Embeddings (`similar`, passages of a book's
  chunks), re-signing for duplicates (`EnqueueUnsigned`) and the search
  page's passages read `book_chunks.body_*` today; they would read the store
  through one function instead.
- *Disk and memory.* The index is memory-mapped from the data volume; the
  page cache it uses is the kernel's, as Postgres's is. Its writer needs a
  memory budget (800 MB for four threads here; books arriving one at a time
  need far less, which was not measured), which counts against the app's
  container and `memlimit`.

That is a re-implementation of what pg_search gives the app for nothing,
for a third of the space it takes. Each of the steps above is a known
pattern, but together they are a milestone's work, and they move a
transactional guarantee out of the database into the app.

**5. Tantivy without the text.** Everything option 4 takes, without the
store: 8.3 GB in place of 25.9 GB (−17.6 GB, two thirds). The index is
the same 7.6 GB (`tantivy-none.json`: 315 MB on the sample, of which
210 MB positions). What it takes beyond option 4:

- *Snippets from the files.* Tantivy's `SnippetGenerator` needs the text,
  so the app reads the chunk's text from the file and marks the query's
  words in it itself. For that a chunk row records where its text is: the
  EPUB entry and the range of the chunk in that entry's text, a PDF's
  pages. An EPUB entry is a few dozen KB; reading one and normalising it
  as extraction did is about a millisecond, a PDF page a few through
  poppler. Bounded in time and size; a file that is gone or changed shows
  the hit without a snippet.
- *Readers of the text read the file.* Embeddings (a book's passages),
  re-signing on a new `SignatureVersion`, and the index rebuild read the
  primary text file again instead of the table, as a restore does now.
- *Rebuild from the files.* A new index format or a new chunk version
  reads every primary text file again: 12 minutes of indexing at 3.19 M
  chunks, plus reading 48,000 EPUBs, which extraction does at a few books
  a second. It runs as a job that snoozes, beside the old index.

## Reasons for the recommendation

(Written before the owner's decision, which weighed 17 GB of disk on a
small server higher than the work.)


- **Nothing inside Postgres makes the text smaller.** pglz is the better of
  the two compressions Postgres 18 offers for TOAST; lz4 is 12 % larger.
  zstd TOAST compression is not in Postgres.
- **What can be saved is a third, not more.** The text halves in a zstd
  store (17.4 to about 8 to 9 GB), but the BM25 index (7.6 GB, two thirds
  of it positions for phrase search) is the same in pg_search and in
  Tantivy. 25.9 GB become 16 to 17 GB.
- **The saving costs transactions.** Moving the text out of the table
  moves the index out of Postgres with it, and with it the guarantee that
  chunks and their index entries commit together, the visibility filter
  inside one query, and the rebuild beside the old index that exists today.
- **The space is not in the backups.** The chunks are derived, and backups
  leave them out; the 17 GB cost disk on the server only.
- **Search is not faster where it counts.** Every query here is under
  40 ms in pg_search; Tantivy in the process saves a few milliseconds, which
  nobody notices.

## If the owner decides for Tantivy

- The store: zstd level 19 in 128 KB blocks (8.9 GB; 322 µs to read a
  chunk). Level 3 in 16 KB blocks, Tantivy's default block size, is 2.4 GB
  larger and compresses about fifty times faster; compressing the whole
  installation's text at level 19 takes three to five CPU hours.
- Fetch for the build: the Rust crates of the chosen binding or of a small
  C interface of GOtome's own (Tantivy at pg_search's version, its
  `zstd-compression` feature, `tantivy-stemmers`), a Rust image for the
  Dockerfile's build stage, and for the toolchain image. Measure the
  installation's real index once with them (`make text-store-spike`), as
  the 0.22.1 here is older than pg_search's fork.
- The issues: the index library and its build; the outbox and the job that
  applies it; search on Tantivy behind `search.Searcher`; the readers of
  chunk text through the store; the rebuild beside the old index; dropping
  the text columns and `book_chunks_bm25` once the new index is complete.
