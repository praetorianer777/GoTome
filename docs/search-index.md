# Rebuilding the search index

Full-text search has two layers, and both can be rebuilt without taking
search away. Neither holds a copy of the books' text: it is read from the
files whenever it is needed (`docs/decisions/text-store.md`).

- **The chunks** of each book's primary text file (`book_chunks`): where
  each piece of about 8,000 characters is in the file, its chapter, pages
  and language. They are made from the file again when a file's
  `chunked_at` is cleared.
- **The index** over the chunks' words, Tantivy in the app, under
  `search/tantivy` on the data volume. It is filled from the files: when a
  book is read, and when a file is handed to it again (`indexed_at`
  cleared).

## What happens by itself at start

`gotome serve` checks both after its migrations, and queues the work in the
background:

1. If the index on the data volume is missing, or was written by an engine
   of another format than this image's (`index_versions`, row `fulltext`),
   it is made anew, and every book's file is read into it again by the
   job "Read the books' text into the search index" (`ingest.index_files`).
   Until a book is read, search does not find it. This is what the first
   start after the update that moved the index out of the database does.
2. If GOtome cuts text into chunks differently than when the chunks were
   made (`index_versions`, row `chunks`, against `ingest.ChunkVersion`),
   every file is marked to be read again, and queued. A book's old chunks
   are found until its new ones replace them.

A database without these rows, a new one or one from before they existed,
is taken to be current for the chunks; the index is read into anew.

So upgrading is: pull the new images and start them. The jobs page shows the
files being read; search answers throughout, from what it has.

## By hand

People with the right to rebuild the index (Editors and Administrators):

- **Jobs page, Search index:** how many books' text search knows, *Read all
  text again* for every book they see, and *Rebuild the index*, which reads
  every book into the index again; until a book is, search finds what the
  index has of it.
- **A library's page:** *Read this library's text again for search*.
- **A book's page:** *Read the text again for search*.

The API is `GET /api/v1/search/status`, `POST /api/v1/search/reread` (with
`library` or `book`, or neither for everything) and
`POST /api/v1/search/rebuild`.

## Time and space

Reading a book into the index means reading its file again, which takes
about as long as extraction did; the indexing itself is fast (12 minutes
for 3.19 million chunks in #244's measurement). The work runs in
twenty-minute passes, one file at a time, with a 64 MB buffer for the
index.

The index takes about as much as the text's words with their positions:
7.6 GB for 48,000 EPUBs (3.19 million chunks), measured in #244. A backup
leaves it out; a restored installation builds it again from the files.
