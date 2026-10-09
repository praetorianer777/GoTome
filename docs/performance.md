# Performance at 50,000 books

GOtome is built for libraries of 50,000 books on modest hardware. What that
looks like, how it is measured, and what an operator can set.

## The main views

`make scale-test` (`backend/test/scale_test.go`) seeds 50,000 books in 20
libraries, with 12,000 authors, 2,500 series, 400 tags, files in three
formats, identifiers, a chunk of text and two vectors per book, reading
state, shelves, duplicate pairs and matches to review. It then asks every
main view of the web app through the API, as an administrator who sees every
library and as an editor who sees 18 of 20, the best of three each. The app
runs inside the test, held to 2 GB of memory.

Each view must answer within one second, and none may read the books, their
files or their chunks from end to end, except the three marked below, whose
answer is about all of them. The test takes the plan of every statement as
Postgres ran it (`auto_explain`, sent to the connection), so a plan that
reads a table whole fails it; `.cache/scale-plans` keeps the plans of the
last run.

Measured on 2026-10-06 on a 12-core machine shared with other work, the
database with `random_page_cost = 1.1` as the compose file sets it:

| View | Administrator | Member | Reads every book |
|---|---:|---:|---|
| library by title | 4 ms | 3 ms |  |
| library by author | 6 ms | 3 ms |  |
| library newest first | 4 ms | 3 ms |  |
| library page 2 | 5 ms | 3 ms |  |
| one library | 4 ms | 2 ms |  |
| filter by author | 3 ms | 2 ms |  |
| filter by tag and language | 6 ms | 3 ms |  |
| filter by status | 9 ms | 5 ms |  |
| filter by year | 3 ms | 3 ms |  |
| facets | 108 ms | 101 ms | counts every book |
| facets of a filter | 44 ms | 40 ms |  |
| count of a filter | 2 ms | 2 ms |  |
| quick search | 70 ms | 69 ms |  |
| author names | 21 ms | 20 ms |  |
| full-text search | 185 ms | 185 ms |  |
| book | 2 ms | 2 ms |  |
| similar books | 236 ms | 225 ms | compares with every book |
| duplicates | 9 ms | 10 ms |  |
| review | 32 ms | 31 ms |  |
| collection | 64 ms | 74 ms |  |
| smart shelf | 13 ms | 21 ms |  |
| collections | 4 ms | 5 ms |  |
| reading statistics | 11 ms | 32 ms |  |
| new books | 6 ms | 9 ms |  |
| search by description | 107 ms | 107 ms | compares with every book |
| clean-up: authors | 237 ms | 219 ms | reads every credit and title |
| clean-up: titles | 177 ms | 159 ms | reads every credit and title |
| trash | 1 ms | 2 ms |  |
| jobs | 0 ms | 1 ms |  |
| libraries | 21 ms | 21 ms | counts every file |
| users | 1 ms | – |  |

The seed has one chunk of text per book, where a real book has about 75: the
full-text search's plans are those of a full library, its timings at full
size are those of [the search decision](decisions/search-engine.md) (under
170 ms at p95 for every kind of query at 50,000 books).

## What made the difference

Before #75 the library list read and sorted every book for each page (78 ms),
the facets took 600 ms, and a smart shelf 1.7 s:

- **Visibility the planner can see into.** Every query keeps to the
  viewer's libraries through `visible_library_ids`. Called in a select
  list (`IN (SELECT visible_library_ids(...))`) it is opaque, and the
  planner guessed that half the books might be visible whoever looked. In
  `FROM` (`IN (SELECT * FROM visible_library_ids(...))`) the SQL function is
  inlined and planned with the libraries it reads.
- **An index for each order across libraries.** The ones that lead with
  `library_id` serve a list of one library; `books_sort_idx`,
  `books_author_idx` and `books_added_idx` (migration 00032) let a list of
  every library stop after a page.
- **Status and rating as EXISTS.** A filter on the viewer's own status
  looked the status up once per book on its way through the list; asked as
  "has a row that says so", it is matched as a set.
- **An index on the language as filters compare it**, which gives the
  planner its statistics: it took one language for one book in 50,000.
- **The facets at once.** Their nine counts run concurrently, each on a
  connection of its own: about the slowest of them rather than their sum.
- **`random_page_cost = 1.1`.** Postgres's default of 4 is for spinning
  disks; with it, joining a shelf of 2,000 books read all 50,000 rather than
  looking 2,000 up. GOtome's tables are mostly in memory, and a NAS with an
  SSD reads at random almost as fast as in order.

## Memory

- **The app.** When the container has a memory limit, `gotome serve` sets
  Go's soft memory limit to three quarters of it (`internal/memlimit`), so
  the heap is collected harder rather than outgrowing the limit; the rest
  is for ONNX Runtime and the programs extraction runs, which share the
  container. `GOMEMLIMIT` overrides it. 2 GB is enough for the views above
  and the background work.
- **Background work** runs at fixed concurrency (`internal/jobs`): one scan,
  two extractions, one embedding pass at a time. Each program extraction
  runs (poppler, ffprobe, ffmpeg) is held to its own address space, time and
  output (`internal/procexec`). Embedding uses `GOTOME_EMBED_THREADS`
  threads, half the CPUs unless set.
- **The database.** The ParadeDB image sizes Postgres's memory when the
  database is first made, from the machine's memory: a quarter for
  `shared_buffers`, and `work_mem` and `maintenance_work_mem` to match. It
  reads the machine's memory, not a container limit: if you give the `db`
  container a limit, set those three with `-c` in its `command` to fit it.
