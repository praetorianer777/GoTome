# Rebuilding the search index

Full-text search has two layers, and both can be rebuilt without taking
search away:

- **The text** of each book's primary text file, cut into chunks
  (`book_chunks`). It is read from the file again when a file's
  `chunked_at` is cleared.
- **The index** over those chunks (`book_chunks_bm25`, ParadeDB's
  `pg_search`). It is built from the chunks alone; the files are not read.

## What happens by itself at start

`gotome serve` checks both after its migrations, and queues the work in the
background:

1. If the database image brings a newer `pg_search` than the database has
   installed, it runs `ALTER EXTENSION pg_search UPDATE`.
2. If that version differs from the one the index was last built with
   (`index_versions`, row `pg_search`), it queues `search.rebuild_index`.
   The job runs `REINDEX INDEX CONCURRENTLY book_chunks_bm25`: the new index
   is built beside the old one, which searches keep using until it takes
   its place. The version is recorded once the rebuild is done; until then,
   every start queues it again, and a queued one is not queued twice.
3. If GOtome cuts text into chunks differently than when the chunks were
   made (`index_versions`, row `chunks`, against `ingest.ChunkVersion`),
   every file is marked to be read again, and queued. A book's old chunks
   are found until its new ones replace them.

A database without these rows, a new one or one from before they existed,
is taken to be current.

So upgrading is: pull the new images and start them. The jobs page shows the
rebuild and the files being read again; search answers throughout, from what
it has.

## By hand

People with the right to rebuild the index (Editors and Administrators):

- **Jobs page, Search index:** how many books' text search knows, *Read all
  text again* for every book they see, and *Rebuild the index*.
- **A library's page:** *Read this library's text again for search*.
- **A book's page:** *Read the text again for search*.

The API is `GET /api/v1/search/status`, `POST /api/v1/search/reread` (with
`library` or `book`, or neither for everything) and
`POST /api/v1/search/rebuild`.

## Time

At 50,000 books the index took 19 minutes to build in the benchmark of
`docs/decisions/search-engine.md`; reading all text again takes as long as
extraction did. A rebuild stopped half-way, by a restart, leaves an unused
copy of the index (`book_chunks_bm25_ccnew`), which the next rebuild drops
before it starts.
