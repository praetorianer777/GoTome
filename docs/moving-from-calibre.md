# Moving from Calibre

GOtome reads a Calibre library in place: the files stay where they are,
and the metadata you put into Calibre comes along.

## How it works

1. Mount the Calibre folder (the one with `metadata.db` and the
   `Author/Title (id)/` folders) into the GOtome container, as for any
   existing folder, and add it under **Libraries** as an existing folder.
   It stays read-only unless you make it writable; the import does not
   need that.
2. Let the library be scanned. GOtome keeps the formats of one Calibre
   folder together as one book, whatever the files are called.
3. Under **Libraries**, choose **Import from Calibre**, pick the library
   and look at the preview.
4. Choose what to take over and start the import.

Only an administrator can do this.

## What the preview says

The preview reads `metadata.db` and writes nothing. It counts the books
Calibre lists, those with a file in GOtome, their files by format, covers,
series, tags and rated books, and lists the custom columns. Books are
matched to files exactly, by Calibre's own path, file name and format, and
never by title. A book none of whose files GOtome knows is left out, and
the preview names it; usually the library has not been scanned yet, or the
files are gone from the folder.

## What is taken from Calibre

Title, authors (with Calibre's author sort), series and position, tags,
publisher, publication date, language, identifiers (an ISBN, and others as
identifiers), the description (Calibre's HTML as plain text) and the cover
(`cover.jpg` in the book's folder). GOtome works out its own sort title.
Calibre's "no date" is ignored.

This counts for more than what the files themselves say, so a title in
Calibre replaces one read from an EPUB. It counts for less than anything
you or a metadata provider set in GOtome: those fields stay, and the
result of the book says so. Nothing is locked, so an edit you make in
GOtome later wins over the next import too.

In a library you made writable, the changes are written into EPUBs like
any other edit of a book. In a read-only library, which is the default,
nothing in the folder is touched.

### Ratings

Off unless you tick it. Calibre's 0 to 10 becomes whole stars, a half star
rounded up, as your own rating, and only for books you have not rated.

### Custom columns

Each custom column is listed with its type and how many books have a
value. For each you choose to leave it out or to turn it into tags (each
value a tag; a yes/no column gives its name when it is yes). A yes/no or
date column can instead set your reading status: yes, or any date, is
"completed", with the date as the day you finished. Your status is only
set for books you have not started.

## Running it again

An import is a background job with an outcome per book: changed,
unchanged, left out or failed. It is safe to run again, and you can after
more changes in Calibre: choose the library again, preview, import. Only
what changed is touched, and only where GOtome holds no newer value of a
person's or a provider's.

## Calibre may keep running

`metadata.db` is opened read-only and immutable: GOtome takes no lock and
writes no file next to it. Read it while Calibre is closed if you can; a
copy Calibre is writing at that moment may be read half done, and running
the import again corrects that.

GOtome does not watch `metadata.db` for changes. Run the import again when
you have changed things in Calibre.

## Troubleshooting

- "holds no Calibre library": the folder has no readable `metadata.db`, or
  the path in the library is a subfolder of the Calibre folder.
- Books left out in the preview: scan the library again, then preview.
- A cover missing: GOtome takes `cover.jpg` from the book's folder only.
