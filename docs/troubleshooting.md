# Troubleshooting

Most answers start in the log:

```bash
cd deploy
docker compose ps
docker compose logs --tail 100 app
```

`GOTOME_LOG_LEVEL=debug` in `.env`, then `docker compose up -d`, makes it
say more. `docker compose exec app gotome version` names the version.

## GOtome does not start

| The log says | What to do |
|---|---|
| `GOTOME_SECRET_KEY_FILE is not set` | The compose file is older than the secret key. Take the current `docker-compose.yml`, which sets it and runs `init`. |
| `GOTOME_SECRET_KEY_FILE holds another key than the one GOtome's secrets were encrypted with` | The `secrets` volume is not the one this database was used with: a new volume, or a restore that left out the key. Put back the `secret-key` from the backup ([backup-and-upgrade.md](backup-and-upgrade.md)). Without it the stored tokens are lost: only a new installation starts. |
| `GOTOME_... is "...", want ...` | A variable in `.env` or the override file has a value GOtome cannot use; the message says what it wants ([configuration.md](configuration.md)). |
| `init` exited with an error | It cannot write into the `secrets` volume. It runs as root to do that: check that nothing overrides its `user`. |

**`db` restarts again and again**, or the app loses its connections now
and then: Postgres is being killed for memory. See
[platforms.md](platforms.md#with-little-memory) and the **This machine**
panel on the Overview page.

**`db` stops and does not start, the log saying `No space left on
device`**: the disk is full. Free some (`docker image prune` removes old
images after updates; empty the trash), then start it again; Postgres
recovers by itself once there is room.

## Signing in and changes

- **Every change is refused with "This request came from another site and
  was refused"**: a reverse proxy changes the host name on the way. It
  must pass on the `Host` the browser used
  ([getting-started.md](getting-started.md#a-reverse-proxy-and-https)).
- **Signing in seems to work, then the sign-in page comes back**: the
  browser does not send the session cookie back. It is marked Secure when
  the request came over HTTPS or the proxy said so with
  `X-Forwarded-Proto: https`; a proxy that says so for a page opened over
  plain HTTP makes a cookie the browser keeps to itself. Open GOtome over
  HTTPS, or fix the proxy.
- **"Too many failed sign-in attempts"**: after several wrong passwords,
  an account and an address are refused for a while. Behind a reverse proxy everyone
  shares the proxy's address. Wait a few minutes.
- **Someone forgot their password**: an administrator sets a new one on the
  Users page. There is no other way: if the only administrator forgot
  theirs and has no single sign-on, a backup made while it was known is
  the way back.
- **Single sign-on fails**: [single-sign-on.md](single-sign-on.md#when-it-fails).
  Locked out after turning passwords off:
  `GOTOME_FORCE_PASSWORD_LOGIN=true`.

## A library finds no books

On the Libraries page, the library's line says what its last scan found.

- **"There is no such folder inside the GOtome container"** when adding
  it: the folder is not mounted, or under another path. The path is the
  one after the colon in the compose file's mount (`/books`), not the
  path on the host.
- **The scan finds 0 files**: GOtome cannot read the folder. Check from
  inside the container: `docker compose exec app ls /books`. A
  "Permission denied" is the folder's permissions: the app runs as user
  10001 ([getting-started.md](getting-started.md#2-mount-your-books)).
- **Some files are passed by**: only EPUB, PDF, MOBI, AZW, AZW3 and audio
  (M4B, M4A, MP3, FLAC, Ogg, Opus) are taken in, and folders whose names
  start with a dot are not looked into.
- **New files are not there yet**: libraries are scanned every
  `GOTOME_SCAN_INTERVAL`; **Scan now** does it at once.
- **Books have files missing**: the files were moved or deleted from the
  folder since. They come back when the files do. A file moved within the
  library is found again by its content.

## Files that cannot be read

When GOtome cannot read a file, its book still exists, under the file's
name, but without the details, cover and text the file would give. Where
to see them:

- The book's page, under **Files**: "Could not be read", with the reason.
- The Libraries page: "Files that could not be read", with **Read them
  again**.
- The jobs page, under **Failed**; the Overview counts them too.
- Editors are notified ("Files that cannot be read").

Common reasons:

| Reason | What it means |
|---|---|
| not the format its name says | The file's first bytes are not those of an EPUB (a zip), a PDF, and so on: a download that went wrong, or a file renamed. GOtome refuses it, as it would refuse such an upload. |
| a broken or truncated zip, a missing package document | The EPUB is damaged. Other readers usually fail on it too. Replace the file. |
| too many entries, too large when unpacked, a name that leaves the archive | GOtome refuses archives built to harm whatever unpacks them: over 20,000 entries, over 4 GB unpacked, an entry inflating to more than 200 times its size. A real book does not come near these. |
| a program timed out or ran out of memory | PDFs and audio files are read by separate programs that get two minutes and 1 GB each. A huge scanned PDF may exceed that. |
| encrypted, DRM | GOtome cannot read the text of a book with DRM: the file fails, or its book has no text to search. |

A file that failed is not tried again by itself. After replacing it in
the folder, the next scan reads it. After an update that reads a format
better, **Read again** (on the book) or **Read them again** (on the
library) tries again.

## E-reader findings and fixes

GOtome measures every EPUB for what makes e-readers slow or crash, and
lists it on the book's page as "May be slow on e-readers". The library's
filter **On e-readers** finds every such book.

| Finding | Fix | What the fix does |
|---|---|---|
| One chapter file holds several megabytes | **Split into chapters** | Splits it at its chapters, or else between paragraphs, into parts of at most 260 KB, with the table of contents and links following. |
| Images of tens of megapixels, or megabytes each | **Scale the images down** | Scales JPEG and PNG images to at most 1600 pixels on their longer side. |
| Embedded fonts of megabytes | **Remove the fonts** | Removes the fonts; the e-reader uses its own. |
| Files the EPUB lists but lacks | **Repair the list of files** | Takes what is missing out of the EPUB's list. |
| Chapter files that are not well-formed XHTML, which some readers (the Tolino's among them) refuse | **Repair the chapter files** | Writes each such file again as well-formed XHTML, only where its text stays the same. |
| Entries of the table of contents that lead nowhere | **Repair the table of contents** | Points an entry at its chapter's start where only the place in it is gone, and takes it out where the chapter itself is gone. |

Fixing needs an editor and a writable library. The fixed EPUB replaces
the file, keeping its book, details and reading places; the file as it
was goes into the trash, from where it can be restored beside the fixed
one until the trash is emptied. A device that had the book gets the fixed
one at its next sync. Several books are fixed at once from the library's
selection.

## Search does not find a book

- The book's text may not be in the index yet: the Overview's **Texts in
  the search index** and the jobs page show how far it is.
- Search finds books with every word of the query, in any form English or
  German stemming knows ("running" finds "runs"), and phrases in double
  quotes. Irregular forms ("ran") are other words to it.
- Books of libraries you do not see are not found.

When the index seems wrong or incomplete, **Rebuild the index** on the
jobs page reads every book into it again, while search keeps working.
[search-index.md](search-index.md) explains the layers and what an update
rebuilds by itself.

## Similar books on slow hardware

Working out what books are about runs a language model over sixteen
passages of every book: a few seconds per book on an ordinary CPU, much
longer on a small ARM board, and a day or more for a large library. It
runs in the background, one book at a time, in five-minute turns, and
nothing else waits for it. What there is to do when it is too slow or
takes too much from the machine:

- **Pause it**: **Pause** on the jobs page, or **Work out what books are
  about** off in the settings. Everything else works; similar books and
  the search by description are shown for books already done. **Start**
  goes on where it stopped.
- **Fewer threads**: `GOTOME_EMBED_THREADS` (half the CPUs by default)
  leaves the rest of the machine free.
- **Another model**: on a CPU without int8 instructions (older x86, some
  ARM boards), `multilingual-e5-small-fp32` may be faster than the default
  int8 model, at four times the download and more memory. Measure it on the
  Overview's pace ("in the last hour") before and after: going back costs
  nothing, as each model's vectors are kept.
- **A GPU elsewhere**: an Ollama server with a GPU runs `bge-m3`
  ([embedding-server.md](embedding-server.md)).
- **No download**: with `GOTOME_OFFLINE=true` the model is not downloaded,
  and the settings page says the model cannot run. Copy the model's
  folder into `/data/models` from another installation, or allow the
  download once.

The model settings page shows for each model whether it runs here, its
download size and memory, and how many books have its vectors.
