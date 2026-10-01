# Changelog

What changes for someone who runs or uses GOtome. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Every installation gets its own random database password on its first
  start, written by a short-lived `init` container and kept in the `secrets`
  volume. There is no default password any more. An installation that set
  `POSTGRES_PASSWORD` before keeps it: leave the variable set for the first
  start after upgrading, and `init` writes that one instead.

- GOtome runs as two containers, the app and its database, started with one
  `docker compose` file. Images are published for amd64 and arm64.
- First-run setup creates the administrator account in the browser.
- Sign in with user name and password. Sessions last 30 days without use, and
  repeated wrong passwords are slowed down.
- Three roles: Administrator, Editor and Reader.
- A web app with light and dark colours that also works on a phone. The library
  view is still empty: importing books comes next.
- Libraries. An administrator adds a library that GOtome manages itself, or
  points GOtome at an existing folder, which it only reads unless allowed to
  change files. A library is shared with everybody or private to its owner and
  members. Removing a library from GOtome never deletes its folder or files.
- Scanning. GOtome looks through a library's folder when the library is added,
  every six hours (`GOTOME_SCAN_INTERVAL`, `0` to switch it off), and when an
  Editor or Administrator presses "Scan now". It takes in EPUB, PDF, MOBI, AZW3
  and audio files, keeps the formats of one title and the parts of an audiobook
  together as one book, follows files that were moved or renamed, and marks
  files that are gone as missing. It never deletes, moves or changes a file.
- Book details are read from EPUB, PDF, MOBI, AZW and AZW3 files: title,
  authors, series, publisher, language, description, tags, ISBN, page count
  and cover. A PDF without a text layer (a scan) is imported and marked as
  such. A Kindle file protected by DRM is imported with its details and cover
  and marked as protected; its text is not read. The app image now includes
  poppler to read PDFs.
- Browsing. The library page shows the books of every library, or of one,
  with their covers or as a list, sorted by title, author or when they were
  added, and loads more as you scroll. A book's page shows what is known about
  it and its files, which can be downloaded; an interrupted download resumes.
- Audiobooks in M4B, M4A, MP3, FLAC, Ogg and Opus: title, author, narrator,
  genre, duration, chapters and cover are read from their tags. The parts of a
  book in several files are put in order by their track and disc numbers. The
  app image now includes FFmpeg, which makes it about 600 MB larger.
- Uploads. Editors and Administrators add books on the new "Add books" page,
  by picking files or dropping them on it, and see each one's progress. A file
  goes into a managed library, into a folder named after its book, next to the
  book's other formats and parts. A file the uploader can already see in any
  library is not stored again; the page points to the book that has it. An
  upload that breaks off leaves nothing behind. One file may be up to 4 GiB
  (`GOTOME_UPLOAD_LIMIT_MB`); a reverse proxy in front of GOtome needs to
  allow that much too (nginx: `client_max_body_size`).
- Filters. The library page has a panel to narrow the books by author,
  series, tag, language, decade of publication and format, each with how many
  books have it; values of one field add up, fields narrow each other. The
  address carries the filters, so a filtered view can be bookmarked or shared.
  On a phone the panel is behind a "Filters" button.
- Quick search. The box in the header finds books by title, author or series
  as you type, even misspelt or only begun: "Sandersen" finds Brandon
  Sanderson. Press "/" to get to it from anywhere, the arrow keys to pick a
  book and Enter to open it. It answers in well under a tenth of a second on
  50,000 books.
- Settings. Administrators set the language book details are fetched in and
  the Google Books and Hardcover keys on a new Settings page. Keys and tokens
  are stored encrypted and are never shown again, in the app or in the logs.
  The encryption key is made by the `init` container on the next start and
  kept in the `secrets` volume, readable by the app only; back that volume up
  with the database. GOtome refuses to start with a key other than the one its
  secrets were encrypted with.
- Users. Administrators add accounts on the new Users page, change their role,
  set a new password, sign them out everywhere, and disable them; a disabled
  account is signed out at once and cannot sign in. The last administrator
  cannot be demoted or disabled.
- Your account. A click on "Signed in as …" opens your own page: change your
  password, which signs you out everywhere else, and see where you are signed
  in, signing out a browser you no longer use.
- Storage quotas. An administrator gives an account a limit on what it may
  upload, on the Users page, which also shows what each account's uploads take
  up. An upload that would pass the limit is refused with a message saying how
  much is used. Files that are gone from disk no longer count. Without a limit,
  there is none.
- Background jobs. Editors and Administrators see on the new Jobs page what
  GOtome is doing: scans and the reading of files, with their state, attempts
  and errors, updated while they run. A failed job can be tried again, a
  waiting or running one cancelled. A book's page says when one of its files
  could not be read, and the library page how many could not; both can have
  them read again. Readers see neither the jobs nor the errors.
- The book page updates by itself while its files are still being read.
- Editing a book. Editors and Administrators change a book's title, people,
  series, publisher, date, language, pages, tags, identifiers, description
  and cover. Names already in the library are offered while typing; a new one
  is added. Every field says where its value came from (which file, the file
  name, or by hand), and what was changed by hand is locked, so reading the
  files again does not undo it. A lock can be taken off again.
- Edited details are written into the book's EPUB files, in managed libraries
  and in external ones that GOtome may change: title, people, series,
  identifiers, language, date, publisher, description, tags, and a cover
  chosen by hand. The book's text is never touched, and a file is only
  replaced once the new one is complete and checked. Files in read-only
  libraries keep what they say.
- Finding details online. On a book's page, "Find details online" asks
  OpenLibrary what it knows, ranks the results by how well they fit, and
  shows field by field what one would change. Tick what to take; locked
  fields stay, and each field taken says which source it came from. Covers
  are shown through GOtome, so the browser contacts nobody else. Works for
  books without files too.
- New books are looked up by themselves once their files are read. A sure
  match (by default one with the same ISBN, or nearly as good) fills in what
  the book is missing — description, date, publisher, people, cover and the
  like — and changes nothing it already has or that is locked; a doubtful
  one waits for review. The administrator's settings switch this off, set
  how sure is sure enough, and choose the sources.
- `GOTOME_OFFLINE=true` keeps GOtome from asking any metadata source on the
  internet.
