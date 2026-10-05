# Search engine benchmark

The harness behind `docs/decisions/search-engine.md` (#14). It measures three
candidates for GOtome's full-text search on the benchmark corpus
(`spikes/corpus`), under the limits a home server sets. Nothing here runs in the
gate.

```bash
make corpus                                   # the base corpus, once (downloads)
make search-spike-extract                     # EPUBs to chunks, about two minutes
make search-spike ENGINE=pgsearch BOOKS=0     # one engine at the corpus's size
make search-spike ENGINE=pgsearch BOOKS=50000 # and at 50,000 books
make search-spike-report                      # the results as Markdown tables
make search-spike-test                        # the harness's own tests
```

## What it does

`extract` reads every EPUB with the app's own reader (`internal/format/epub`)
and cuts the text into chunks of about 8,000 characters at paragraph breaks,
the way #53 will store them. The result goes to `.cache/corpus/chunks.jsonl.gz`.

`bench` builds one engine's index and measures it:

- **Build**: loading the text and building the index, with each step's time.
- **Disk**: the index alone, and everything the engine stores with the text.
- **Memory**: the highest anonymous memory of the engine's container, sampled
  every 200 ms from its cgroup, during the build, the queries and a rebuild.
  Page cache is left out because the kernel takes it back at the limit; the
  container is killed only if anon does not fit.
- **Latency**: each query runs once to warm up and then five times. The
  latencies are reported per kind (stemmed, phrase, fuzzy, filtered by
  library, stemmed with snippets, and repaired) as p50, p95 and the maximum.
  Every query asks for the top 10 chunks, all words required. Repaired
  queries are the fuzzy ones with each word looked up in a vocabulary table
  through `pg_trgm` first and the words found searched with snippets; Bleve,
  which highlights its fuzzy hits itself, has none.
- **Samples**: the top three hits of each query with their titles, and
  snippets where asked, to judge stemming, typo repair and highlighting.
- **Plans**: `EXPLAIN ANALYZE` of one query of each kind, which shows whether
  the library filter is applied inside the index.
- **Rebuild**: `REINDEX` of the search index, which is what a version of the
  engine with a new index format costs.

At 50,000 books the corpus is read as often as it takes, each copy under new
book IDs. That is the same text `make corpus-50k` writes, without the 14 GB
of EPUBs on disk. The vocabulary does not grow with the copies, so term
dictionaries are smaller than in a real library of that size.

## The candidates

| Engine | How it is set up |
|---|---|
| `pgsearch` | ParadeDB's `pg_search` in the image the compose file pins. One text column per language, each with its stemmer (English, German, and no stemmer for the rest), and `library_id` in one BM25 index. A query asks all three columns. Fuzzy queries use pg_search's own fuzzy match; repaired ones a vocabulary of the words as written. |
| `fts` | Postgres's own full-text search in the same image: a `tsvector` stemmed in the chunk's language under a GIN index, ranked by `ts_rank_cd`. Typos are repaired by replacing each word with the indexed words `pg_trgm` finds like it, from a vocabulary of the stemmed words. |
| `bleve` | Bleve v2 in the harness's process, as it would run inside the app. One field per language with its analyzer, text and term vectors stored for highlighting, the library as a keyword field. |

## Limits

`bench.sh` gives the engine's container 2 GB of memory without swap and four
CPUs. Postgres gets 512 MB of shared buffers, 512 MB of `maintenance_work_mem`
and three parallel maintenance workers; Bleve runs with `GOMEMLIMIT=1600MiB`.
The host is a twelve-core x86-64 machine that is doing other work, so the
times are comparable with each other and not exact.

Both vocabularies are built with `ts_stat` one copy of the corpus at a time:
in one statement over 50,000 books the backend did not fit in 2 GB.

pg_search needs the query text as a constant; prepared statements would run
on a generic plan after five executions and fail. The harness connects with
`plan_cache_mode=force_custom_plan`, as the app will have to.
