# Text store benchmark

The harness behind `docs/decisions/text-store.md` (#244, #268). It measures,
on the same chunks, what a book's text takes where it could be kept: in
Postgres as now (TOAST with pglz) and with lz4, in a zstd store of GOtome's
own, and in Tantivy's document store beside its index; and Manticore Search
as a search engine in a container of its own, with the text indexed only. Nothing here runs in the gate: it
reads a library of EPUBs, which only the owner has.

```bash
make text-store-spike LIBRARY=/path/to/epubs                # every step
make text-store-spike LIBRARY=/path/to/epubs STEPS="pglz lz4" # some of them
make text-store-spike LIBRARY=/path/to/epubs STEPS="extract tantivy-none manticore" # #268
```

The library is mounted read-only. Everything the harness writes goes to
`.cache/text-store` (the chunks, the Tantivy indexes, the Rust build); the
numbers go to `spikes/text-store/results/`. The chunks are a person's books:
they stay in `.cache`, and the results hold sizes and times only, no text,
titles or file names.

## The steps

| Step | What it does |
|---|---|
| `extract` | Lists the library's EPUBs in name order, takes every 24th, reads each with the app's EPUB reader (`internal/format/epub`) and cuts it with the app's `textproc.Split`, as extraction does. Writes `chunks.jsonl.gz`. |
| `pglz`, `lz4` | Starts a throwaway Postgres from the image the compose file pins (container, volume and network `gotome-text-store-spike*`, removed afterwards; no host port), creates `book_chunks` as migrations 00021 and 00022 make it (without the foreign keys), sets the TOAST compression of the text columns for `lz4`, loads the chunks with `COPY`, builds `book_chunks_bm25`, vacuums, and reports the heap, TOAST, B-tree and BM25 sizes. Then times the searches of `queries.json` the way `internal/search` asks them. |
| `zstd` | `zstd_store.py` (Python 3.14's `compression.zstd`) compresses the chunks per chunk, per chunk with a dictionary trained on a tenth of the books (112 KB and 1 MB), per book, and in blocks of 16 KB and 128 KB across chunks, at levels 3 and 19; and times reading one chunk back from each. |
| `tantivy` | `tantivy/` (Rust, Tantivy 0.22.1 from the local crate cache, built with `cargo --offline`) indexes the chunks with pg_search's tokenizers (simple, lower case, English and German Snowball), the book and the library as raw fields, and the text not stored, in the lz4 store, or in the zstd store (level 3 in 16 KB blocks, level 19 in 128 KB blocks). Merges to one segment, reports the files by kind and times the same searches, with snippets from the store. |

| `tantivy-none` | The Tantivy index without a store again, each query 20 times, for the best 1,000 and the best 10, with snippets made from the text handed in (read back from the chunks for the hits), as Manticore makes them. Writes `results/tantivy-none-handed-in.json`, with the process's memory. |
| `manticore` | Starts a throwaway Manticore Search from the pinned image (`manticoresearch/manticore:29.9.0` by digest, which the owner pulls; the step only checks it is there) with `manticore.conf`, on the spike's network without a host port, under the same limits as Postgres. `manticore-load` creates a real-time table per language (`libstemmer_en`, `libstemmer_de`, none: morphology is a table's setting, not a field's), each `body text indexed` (not stored) with `book_id` and `library_id` attributes and a small `rt_mem_limit`, and a distributed table over the three; inserts the chunks through `/bulk`, flushes and optimizes each to one disk chunk. The container is restarted, then `manticore-search` checks stemming (`CALL KEYWORDS`, hits of two forms), times the searches of `queries.json` through `/search` and the snippets through `CALL SNIPPETS` with the text handed in, and times moving, deleting and writing again twenty books' chunks. A loop samples `docker stats` throughout; the result has the memory by phase, the files by kind, the restart time. Writes `results/manticore.json`. |

## Limits

Postgres gets the limits of #14: 2 GB of memory, four CPUs, 512 MB of shared
buffers. Tantivy's writer gets four threads and 800 MB. Each query runs once
to warm up and five times; the page cache is warm for both. The host is a
twelve-core x86-64 machine doing other work, so times compare the engines
with each other and are not what a dedicated server shows.

Manticore gets the same 2 GB and four CPUs, and 128 MB of RAM chunk per
table. Its times are taken by the harness over HTTP on a Docker network, as
the app would see them; `tookMs` is Manticore's own.

pg_search 0.25.11 builds on a fork of a newer Tantivy than 0.22.1, so the
Tantivy numbers are for the engine underneath, not for pg_search's exact
version.
