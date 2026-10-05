# Full-text search engine

Decided in #14, 2026-10-04.

## Question

GOtome searches the text of up to 50,000 books: stemmed in English and
German, by phrase, with typos, filtered to the libraries the reader may see,
with a snippet that shows where the words are. Everything has to run in the
two containers of a deployment, on hardware like a NAS. Three candidates were
measured on the benchmark corpus: ParadeDB's `pg_search` (BM25 on Tantivy,
inside Postgres), Postgres's own full-text search with a `pg_trgm` vocabulary
for typos, and Bleve (pure Go, in the app's process).

## Decision

GOtome searches with **pg_search**, in the database image the compose file
already pins: `paradedb/paradedb:0.25.11-pg18`, by digest
`sha256:a9cbdcfd8a1c349ab21590fd6d6dcbe7da489878df6502922d032dd64c1a7ae7`
(Postgres 18.6, pg_search 0.25.11, pgvector 0.8.4). The image does not
change for this decision.

- A chunk's text goes into one column per language (`body_en`, `body_de`, and
  `body_xx` for every other language), each indexed with its stemmer
  (`pdb.simple('stemmer=english')`, `'stemmer=german'`, none); `library_id`
  is in the same BM25 index. A query asks all three columns, all words
  required (`&&&`), phrases with `###`, ranked by `pdb.score`.
- Typos are repaired before the search, not by pg_search's fuzzy match. Each
  word of the query is looked up in a vocabulary table (`words`, every word
  of the chunks with the number of chunks it is in) through `pg_trgm`, and
  the words found are searched like any others. That ranks by BM25 and gives
  snippets; pg_search's own fuzzy match does neither (#55).
- The vocabulary is kept current as chunks are written (#53), not built in
  one statement: see the limits below.
- The app sends search queries with `plan_cache_mode=force_custom_plan` (or
  unprepared). pg_search needs the query text as a constant; a prepared
  statement fails with "the right-hand side of the `&&&` operator must be a
  text" once Postgres switches it to a generic plan after five runs.

## Measurements

The corpus is 2,011 public-domain EPUBs (1,200 English, 608 German, 203
French), read with the app's EPUB reader and cut into chunks of about 8,000
characters at paragraph breaks. At 50,000 books it is read 25 times under new
book IDs. Each engine ran in a container with 2 GB of memory, no swap and four
CPUs; Postgres with 512 MB of shared buffers and 512 MB of
`maintenance_work_mem`. Every query asks for the top 10 chunks; each ran once
to warm up and then five times.

| Engine | Books | Chunks | Text | Build | Index on disk | Total on disk | Peak anon, build | Peak anon, queries | Rebuild |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| pg_search | 2,011 | 94,107 | 720 MB | 3 min | 231 MB | 889 MB | 715 MB | 561 MB | 47 s |
| Postgres FTS + pg_trgm | 2,011 | 94,107 | 720 MB | 3 min | 934 MB | 1,514 MB | 850 MB | 435 MB | 18 s |
| Bleve | 2,011 | 94,107 | 720 MB | 84 s | 1,527 MB | 1,527 MB | 880 MB | 816 MB | full build |
| pg_search | 50,000 | 2,338,921 | 17.9 GB | 93 min | 9.5 GB | 21.4 GB | 742 MB | 206 MB | 19 min |
| Postgres FTS + pg_trgm | 50,000 | 2,338,921 | 17.9 GB | 82 min | 17.3 GB | 31.3 GB | 851 MB | 96 MB | 8 min |
| Bleve | 50,000 | 2,338,921 | 17.9 GB | 44 min | 34.8 GB | 34.8 GB | 996 MB | 354 MB | full build |

"Index on disk" is the search index alone; for FTS it counts the
`tsvector` column (15 GB at 50,000 books), its GIN index and the vocabulary.
"Total" adds the stored text. pg_search's build at 50,000 books is 14 minutes
of loading, 20 of indexing, 5 of vacuum, and 53 of building the vocabulary
from the stored text after the fact, which the app spreads over extraction
instead. Peak anon is the memory the processes held themselves, sampled from
the container's cgroup; the page cache, which the kernel takes back at the
limit, is left out.

Latency in milliseconds, p50 / p95 (max):

| Engine | Books | Stemmed | Phrase | Fuzzy | Filtered | Snippets | Repaired |
|---|---:|---:|---:|---:|---:|---:|---:|
| pg_search | 2,011 | 1.4 / 2.1 (2.7) | 4.1 / 8.0 (8.0) | 36 / 74 (74) | 1.6 / 1.8 (1.9) | 6.4 / 7.3 (7.3) | 16 / 32 (32) |
| Postgres FTS + pg_trgm | 2,011 | 12 / 98 (102) | 54 / 478 (1,000) | 23 / 830 (1,517) | 3.4 / 46 (63) | 39 / 109 (119) | 34 / 500 (772) |
| Bleve | 2,011 | 1.8 / 4.3 (4.7) | 18 / 62 (63) | 3.6 / 5.6 (5.9) | 3.1 / 4.3 (4.6) | 3.0 / 6.2 (6.3) | – |
| pg_search | 50,000 | 5.1 / 13 (13) | 31 / 84 (85) | 76 / 166 (167) | 7.6 / 8.1 (8.4) | 11 / 15 (16) | 21 / 66 (67) |
| Postgres FTS + pg_trgm | 50,000 | 292 / 39,456 (54,009) | 18,810 / 42,482 (46,292) | 2,292 / 64,028 (66,346) | 90 / 23,051 (26,378) | 10,406 / 38,254 (38,510) | 2,998 / 64,914 (70,614) |
| Bleve | 50,000 | 14 / 57 (65) | 452 / 1,816 (1,883) | 52 / 77 (77) | 62 / 98 (100) | 68 / 123 (125) | – |

- **Stemmed**: all words, in any form ("whales harpooned", "Häuser Flüsse",
  "verlorene Liebe", "maison jardin").
- **Phrase**: words in order ("white whale", "der alte Mann", "es war einmal").
- **Fuzzy**: one typo per word, the engine's own fuzzy match at one edit
  ("detectve", "whael harpon", "Kinnder", "elefant", "jardn").
- **Filtered**: stemmed, restricted to 2 of 20 libraries, a tenth of the books.
- **Snippets**: stemmed, with a highlighted excerpt per hit.
- **Repaired**: the fuzzy queries with their words looked up in the vocabulary
  first, then searched with snippets. Bleve highlights its fuzzy hits itself.

How the harness works, and how to run it again, is in
[`spikes/search/README.md`](../../spikes/search/README.md); the raw results,
with the top hits of every query and the query plans, are in
`spikes/search/results/`.

## Reasons

- **It stays fast at 50,000 books.** Every kind of query has a p95 under
  170 ms at 50,000 books, and the queries a person types most (stemmed,
  filtered, with snippets) under 16 ms. Bleve's phrase queries take 1.8 s at
  p95 and grow with the index; Postgres FTS takes seconds to a minute,
  because `ts_rank_cd` has to read the `tsvector` of every matching chunk
  before the top 10 are known, and at 2 GB those reads go to disk.
- **The library filter is applied inside the index.** The plan shows
  `library_id` as a `term_set` in the Tantivy query, next to the words, so the
  top 10 are taken among the visible chunks only. FTS reads every chunk that
  matches from the table and drops the other libraries' rows there.
  This is what keeps search inside `visible_library_ids` without a second
  copy of the rule.
- **It takes the least disk.** 21 GB in all at 50,000 books, text included,
  against 31 GB for FTS and 35 GB for Bleve, which keeps its own copy of the
  text beside the database's.
- **It fits in 2 GB.** Building took at most 0.74 GB, queries 0.56 GB.
- **No second store.** The index is in the database, so it is in the same
  transaction as the rows, the same backup, and the same visibility rules.
  Bleve would be a second store to keep in step with every change and to
  rebuild after a restore.
- **No stop words.** pg_search's tokenizer keeps every word. "es war einmal"
  is found by pg_search only; Postgres's and Bleve's German stop-word lists
  remove every word of it and leave an empty query.
- **Stemming is the same Snowball.** "whales harpooned" highlights "whale"
  and "harpooning", "running horses" highlights "runs" and "horse",
  "verlorene Liebe" highlights "verloren" and "liebe". The top hits of all
  three engines are of the same quality; BM25 ranks no worse than
  `ts_rank_cd`.
- **The repaired typo path ranks and highlights.** pg_search's own fuzzy
  match scores a hit by edit distance alone, the same for a passage that
  names the word once or twenty times, and gives no snippet. Looking the
  words up first and searching the real words gives BM25 order and snippets
  at 21 / 66 ms: "detectve" finds the detective manuals, "elefant" the
  chapter on elephants in a German book about Ceylon.

## Limits

- **Typo repair needs tuning (#55).** Trigrams match short words with swapped
  letters badly: "whael harpon" found nothing, and "jardn" picked "jards"
  over "jardin". Ranking the candidates by edit distance (`levenshtein` from
  `fuzzystrmatch`, which the image has) and by how often they occur, and
  searching only when the stemmed query finds too little, is #55's work.
- **The vocabulary cannot be built in one statement.** `ts_stat` keeps every
  distinct word of what it reads in memory; over all 50,000 books the backend
  was killed by the cgroup's OOM killer at 1.5 GB of its own memory plus the
  shared buffers. Built a corpus copy (2,000 books) at a time, it fits. #53
  keeps it current as chunks are written, and a rebuild goes in batches.
- **A rebuild takes 19 minutes at 50,000 books.** ParadeDB's upgrade guide
  requires `ALTER EXTENSION pg_search UPDATE` after a new image and suggests
  `REINDEX` if queries misbehave after a large version jump; it promises no
  index format across versions. #57 detects a version change at start and
  rebuilds concurrently, beside the old index, which searches keep using
  meanwhile (`docs/search-index.md`). The image changes only in a commit
  that says so.
- **pg_search is pre-1.0.** The SQL used here (the `pdb.*` casts and the
  `&&&`, `###` and `|||` operators) is 0.25's, so the queries are kept in
  one package behind the app's own interface, and a new image is tested
  against them before its pin changes.
- **Not measured on arm64.** The pinned digest is a multi-architecture image
  with an arm64 variant, but there is no arm64 machine here, and times under
  emulation would say nothing. The first arm64 deployment should run
  `make search-spike` at the corpus's size once.
- **The times are relative.** The host is a twelve-core x86-64 machine that
  was running other stacks and swapping during the runs, so all three engines
  were slowed alike; the times compare them and are not what a dedicated
  server shows. The replicated corpus has the vocabulary of 2,011 books, so
  term dictionaries are smaller than in a real library of 50,000.
