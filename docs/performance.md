# Performance at 50,000 books

GOtome is built for libraries of 50,000 books on modest hardware. What that
looks like, how it is measured, and what an operator can set.

## The main views

`make scale-test` (`backend/test/scale_test.go`) seeds 50,000 books in 20
libraries, with 12,000 authors, 2,500 series, 400 tags, files in three
formats, identifiers, a chunk of text and two vectors of 1,024 numbers (the
size of bge-m3's, the largest model) per book, reading
state, shelves, duplicate pairs, matches and genres to review, and ten
feeds of 5,000 items for each of the two people who ask. It then asks every
main view of the web app through the API, as an administrator who sees every
library and as an editor who sees 18 of 20, the best of three each. The app
runs inside the test, held to 2 GB of memory and two CPUs, and so is the
database (`make scale-test` limits the stack's with `docker update`), as on
the smallest machine GOtome is meant for (#455).

Each view must answer within one second, and none may read the books, their
files or their chunks from end to end, except the views marked below, whose
answer is about all of them (for the genre review, only its count of what
waits per genre: its page may not). The test takes the plan of every
statement as Postgres ran it (`auto_explain`, sent to the connection), so a
plan that reads a table whole fails it; `.cache/scale-plans` keeps the plans
of the last run.

Vectors of 384 numbers and a database on all 12 cores, measured on 2026-11-06 on a 12-core machine shared with other work, the
database with `random_page_cost = 1.1` as the compose file sets it:

| View | Administrator | Member | Reads every book |
|---|---:|---:|---|
| library by title | 5 ms | 3 ms |  |
| library by author | 5 ms | 3 ms |  |
| library newest first | 5 ms | 3 ms |  |
| library page 2 | 8 ms | 3 ms |  |
| one library | 4 ms | 6 ms |  |
| filter by author | 3 ms | 3 ms |  |
| authors | 5 ms | 3 ms |  |
| series | 3 ms | 3 ms |  |
| an author | 1 ms | 1 ms |  |
| an author's books | 2 ms | 2 ms |  |
| an author's facets | 6 ms | 4 ms |  |
| a series | 7 ms | 10 ms |  |
| a series' books | 4 ms | 3 ms |  |
| filter by tag and language | 6 ms | 4 ms |  |
| filter by status | 7 ms | 6 ms |  |
| filter by year | 3 ms | 4 ms |  |
| filter by missing files | 4 ms | 5 ms |  |
| filter by no file on disk | 6 ms | 6 ms |  |
| filter by e-reader findings | 7 ms | 8 ms |  |
| facets | 134 ms | 113 ms | counts every book |
| facets of a filter | 38 ms | 37 ms |  |
| count of a filter | 2 ms | 2 ms |  |
| quick search | 31 ms | 30 ms |  |
| every quick search match | 40 ms | 33 ms |  |
| every quick search match by author | 58 ms | 31 ms |  |
| facets of every quick search match | 163 ms | 90 ms |  |
| author names | 17 ms | 17 ms |  |
| full-text search | 15 ms | 12 ms |  |
| book | 2 ms | 2 ms |  |
| similar books | 329 ms | 243 ms | compares with every book |
| duplicates | 32 ms | 25 ms |  |
| review | 60 ms | 47 ms |  |
| collection | 91 ms | 78 ms |  |
| smart shelf | 12 ms | 21 ms |  |
| collections | 8 ms | 14 ms |  |
| reading statistics | 20 ms | 41 ms |  |
| highlights | 3 ms | 2 ms |  |
| highlights matching words | 27 ms | 27 ms |  |
| new books | 7 ms | 6 ms |  |
| items of followed feeds | 13 ms | 10 ms |  |
| genre review | 39 ms | 36 ms | counts every waiting suggestion |
| genre review page 2 | 33 ms | 38 ms | counts every waiting suggestion |
| genre review of a genre | 35 ms | 37 ms | counts every waiting suggestion |
| search by description | 136 ms | 126 ms | compares with every book |
| clean-up: authors | 265 ms | 233 ms | reads every credit and title |
| clean-up: titles | 170 ms | 167 ms | reads every credit and title |
| clean-up: series | 183 ms | 189 ms | reads every credit and title |
| trash | 1 ms | 1 ms |  |
| jobs | 0 ms | 1 ms |  |
| libraries | 22 ms | 26 ms | counts every file |
| users | 1 ms | – |  |
| e-reader catalogue: newest | 7 ms | 6 ms |  |
| e-reader catalogue: authors | 4 ms | 4 ms |  |
| e-reader catalogue: an author | 5 ms | 5 ms |  |
| e-reader catalogue: search | 30 ms | 31 ms |  |
| overview | 290 ms | 276 ms | counts every book and file |
| overview of a library | 40 ms | 40 ms | counts every book and file |
| overview: work | 166 ms | 145 ms | counts every book and file |

The age rating of accounts (#426) is part of every list's visibility rule.
The scale test rates a third of the books and gives the member a maximum, so
that the rule is worked out for every row; it is written as a comparison
with a once-per-statement `(SELECT age_limit(viewer, sees_all))` because a
function of the row would be called for every row (the filter views took
1.8 seconds that way) and a `COALESCE` on the column would hide it from the
planner's statistics (the filtered facets took 400 ms instead of 50). The
views stay where they were.

The duplicate rules (#466) are a job, not a view. The scale test counts the
classes of the seed's open pairs (`resolvable duplicates`, 24 ms) and runs a
dry run over 907 of them, 20 pairs of each class mixed, on two CPUs: 21
seconds, 22 ms a pair. It is held to 150 ms a pair, and no query of it may
read the books, their files or their chunks from end to end.

With vectors of 1,024 numbers and two CPUs (2026-11-10, #455) the views that
compare vectors and the facets of the quick search became:

| View | Administrator | Member |
|---|---:|---:|
| similar books | 712 ms | 707 ms |
| recommendations | 3,696 ms | 3,412 ms |
| search by description | 405 ms | 406 ms |
| facets of every quick search match | 194 ms | 131 ms |

On the owner's library (48,560 books, 3.19 million chunks, 2 CPUs) the same
views took 2,057 ms, 1,751 ms (facets of every quick search match), 1,314 ms
(a phrase) and over 10 s (description). The seed's one chunk per book does
not reach the phrase search's.

After #456 (same machine, same seed):

| View | Administrator | Member |
|---|---:|---:|
| similar books | 269 ms | 255 ms |
| recommendations | 711 ms | 626 ms |
| search by description | 168 ms | 153 ms |
| facets of every quick search match | 53 ms | 48 ms |

- A vector of 1,024 numbers is 4 KB, over the 2 KB at which Postgres moves a
  value out of the row into the table's TOAST relation, so every comparison
  read two relations and an index per vector (875,000 buffers for one
  similar-books query at 50,000 books). Migration 00073 stores them in the
  row. Alone it takes the query from 1,000 ms to 360 ms; the rest is the
  cosines of 98,000 vectors (75 ms) and a plan that memoized the whole scan
  of each kind (160 MB), which `ListSimilarBooks` avoids by scanning each
  kind in a lateral subquery. Its answer is the former query's
  (`TestSimilarBooksAreTheSameAsTheFormerScanWithLongVectors`).
- The facets of a quick search each looked the matching books up again, 14
  queries side by side on 2 CPUs. They are looked up once and each count
  takes the list (up to 5,000 books; a wider search is looked up by each, as
  before).
- The model of the search by description took over 10 s to open on 2 CPUs, at
  the first search. It is opened in the background at start when its files
  are there (`Service.Warm`), and a search that arrives while it opens waits
  up to 5 s and is then answered 503, "The embedding model is loading", while
  the opening goes on.
- The phrase search is not changed. The scale test cannot show it (one chunk
  per book where the owner's library has 65), and the time could be in the
  engine, scoring the positions of the chunks that hold both words of the
  commonest phrase of ten books (which `perf-check` picks), in the outer
  join, or in the snippets, which read the page's files. If it is the
  engine, it needs a change of the engine (a smaller window for a phrase, or
  an index of common word pairs), not a query. Which it is takes the phases
  timed on the owner's machine.

The seed has one chunk of text per book, where a real book has about 75: the
full-text search's plans are those of a full library; it runs on the index
in the app (#265), whose timings at full size are those of
[text-store.md](decisions/text-store.md) (under 11 ms for the best 1,000
chunks of 3.19 million, snippets aside). Its files are not there, so its hits
come without snippets, which a real search reads from the files.

## On a real library

`gotome perf-check` times the same views on an installation's own library,
which a generated one cannot stand in for. It runs inside the app container,
against the running installation's database and full-text index:

```
docker compose exec app gotome perf-check
```

It asks the views as one person, the first administrator unless `-user`
names another, and only reads: the database refuses every write on its
connections, and no job is queued. It is light by design: one request to
warm up, then the best of three per view, one view at a time with
`-pause` (half a second) between, and `-timeout` (10 seconds) for each
request; a view with no answer in that time is reported as "over 10 s" in its
row and the run goes on, ending with an error that names those views; the
first view that answers with a failure ends the run, naming the view and
the status. `-skip "search by description,jobs"` leaves views out, and
`search by description` needs the embedding model to run, here or on a
server. The views marked in the last column read every book, as in the table
above. The views are the list the scale test asks (`internal/perf`); one the
library has nothing to ask about, such as a series page in a library with no
series, says so. The search by description does not use passage vectors.

What it asks about is picked from the library and never printed: the author,
series, tag and language with the most books, the commonest word of the
titles for the quick search, and for the full-text search a common word and
a common two-word phrase from the text of ten books (the titles' words if
their files cannot be read).

The output is a Markdown table you can paste anywhere: the counts of books,
files, authors, series, chunks (the planner's estimate) and vectors; the CPUs
and memory the container sees; PostgreSQL's version and memory settings;
whether embedding runs and which model; and the memory in use before and
after the views, of the app process, the app container and, if the database
role may read its files, the database container (`docker stats`' figure:
without file cache the kernel gives back). It names no title, author,
series, tag, word or ID of the library, and no error does; a test holds the
output to that.

The owner's library, about 48,500 books on a 5 GB LXC container without a GPU,
is yet to be measured; its table goes here.

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
- **The genre review in its order.** Across every genre, a page of the
  review sorted every waiting suggestion and read every book to do so
  (55 ms at 17,000 suggestions); `book_genres_waiting_order` (migration
  00053) lets it stop after a page (under 1 ms).
- **`random_page_cost = 1.1`.** Postgres's default of 4 is for spinning
  disks; with it, joining a shelf of 2,000 books read all 50,000 rather than
  looking 2,000 up. GOtome's tables are mostly in memory, and a NAS with an
  SSD reads at random almost as fast as in order.

## The first load

What a browser downloads to open the app, on a first visit or a hard
refresh (#353). Measured with `make web-build`: the JavaScript `index.html`
loads, and what of it goes over the wire.

| | Files | JavaScript | Sent |
| --- | --- | --- | --- |
| Before | 1 | 798 KB | 798 KB, uncompressed |
| After | 7 | 635 KB | 194 KB gzip, 166 KB brotli |

- **Compressed at build.** `npm run build` ends with
  `web/scripts/compress.mjs`, which leaves a `.br` and a `.gz` beside every
  text file of at least 1 KiB in `dist` (when it saves a tenth). `webui`
  sends the best one the request's `Accept-Encoding` allows, with
  `Content-Encoding` and `Vary: Accept-Encoding`, and the file as it is
  otherwise; nothing is compressed while answering. The PDF.js worker
  (1.2 MB) goes as 300 KB.
- **Pages on demand.** The library, a book's page and sign-in are in the
  first load; every other page is a chunk of its own that loads on its
  first visit (`lazyRouteComponent` in `web/src/router.tsx`), as the
  readers did before. The entry chunk went from 798 KB to 134 KB; React,
  TanStack and openapi-fetch are one `libraries` chunk (362 KB) that
  changes only with them.
- **API answers.** A JSON answer of at least 1 KiB is gzipped for a client
  that accepts it (`httpapi/compress.go`); files, covers, the OPDS feeds
  and the notification stream are sent as they are.

## Budgets of the web app

Measured in the lab, in the browser suite and the web gate: GOtome collects
no field data, by design (#378). A budget is set above today's figure so that
a loaded machine does not trip it, and is raised only on purpose, in the
commit that needs it. The main chunk's went from 165 to 185 KiB for the
player's queue, sleep timer and boost (#420, #421) and batch 2's texts in
both languages. Since #445 only English is bundled; German is a chunk
(`de-*.js`, 80 KiB raw) fetched before the first render when the browser or
the profile asks for it, so the first load fell from 225 to 202 KiB gzip and
from 191 to 174 KiB brotli (the main chunk, 166 to 165 KiB, barely moved: the
German texts had been in a chunk the entry preloads), and the budgets with it,
by the same headroom. The main chunk's budget went from 183 to 186 KiB for
the Highlights page and the readers' selection bar (#408).

| What | Where | Today | Budget |
| --- | --- | --- | --- |
| Main chunk (`index`, raw) | `make check-web` (`web/scripts/budget.mjs`) | 184 KiB | 186 KiB |
| First load, gzip (the entry, what it preloads, the stylesheet) | `make check-web` | 202 KiB | 221 KiB |
| First load, brotli | `make check-web` | 174 KiB | 193 KiB |
| Largest contentful paint | `e2e/tests/vitals.spec.ts` | 0.2 to 0.9 s | 2.5 s |
| Cumulative layout shift | `vitals.spec.ts` | 0.00 to 0.07 | 0.1 |
| Ticking a filter, opening a book, turning a page (the slowest event to the next frame) | `vitals.spec.ts` | 16 to 110 ms | 200 ms |

- The views are the library, a book's page, the text search and the EPUB
  reader. The phone project runs them with the CPU four times slower and a
  network of 9 Mbit/s and 60 ms; the desktop project runs them as they are.
  Only Chromium has the observers (`PerformanceObserver` for
  `largest-contentful-paint`, `layout-shift` and `event`, no package) and can
  be throttled, so the other two projects skip it.
- Each load is measured three times and the median counts; each action three
  times and the fastest counts. One slow measurement on a busy machine is
  not a regression, three are.
- A layout shift of an element that was gone before the browser reported it
  is not counted: that is the book's own text moving inside the reader's
  frame, which the app does not lay out. The library's 0.07 is the strip over
  it appearing once its books arrive.
- The first load is what `index.html` names, compressed the way
  `compress.mjs` does, so the figure is the bytes a browser receives.
- Alongside the budgets, `e2e/tests/layout.spec.ts` holds every main view to
  the layout rules of `docs/design.md` (reflow at 320 pixels and 200 % zoom,
  text spacing, targets, one primary button, visible focus), in all four
  projects, and `web/src/design-lint.test.ts` the type scale and spacing in
  the source.

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
