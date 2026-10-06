# Changelog

What changes for someone who runs or uses GOtome. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Large libraries are fast: at 50,000 books every page answers within a
  quarter of a second, most within a few milliseconds, and the library's
  filters count in about a tenth of a second.
  docs/performance.md has the figures. The database now takes random reads
  for cheap (`random_page_cost = 1.1` in the compose file), and GOtome keeps
  its memory under the container's limit when it has one.
- Sign in through your own identity provider (OpenID Connect, such as
  Authentik, Authelia or Keycloak). An administrator sets it up in
  Settings, where the address to register at the provider is shown. A first
  sign-in makes an account, or takes the one with the address the provider
  verified if you allow that; anyone signed in can link their account from
  their profile. Groups at the provider can decide who is an administrator,
  an editor or a reader. Signing in with a password can then be turned off;
  administrators keep it until one of them has linked an account, and
  `GOTOME_FORCE_PASSWORD_LOGIN=true` brings it back if the provider is
  gone.
- `deploy/backup.sh` backs a running installation up (database, covers,
  managed libraries and the secret key), and `deploy/restore.sh` brings a
  backup back into a new installation, which makes the search again in the
  background. docs/backup-and-upgrade.md says how, and how to upgrade.
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
- Review. Doubtful matches wait on the new Review page, the longest waiting
  first: compare, tick what to take, or reject a match so it is not
  proposed again. It works from the keyboard (a, r, 1–3, j and k). Editors
  see the books of the libraries they may see.
- Changing many books at once. On the library page, Select books, tick
  them or select every book the filter matches, then edit (set the series,
  publisher, date or language; set, add or remove authors and tags), fetch
  details for all of them, or write their details into their EPUB files. It
  runs in the background, up to 5000 books at a time, and a page shows its
  progress and what came of each book. Fields locked on a book are left as
  they are and reported, unless you ask for them to be changed too; a book
  that fails does not stop the others.
- Your own status for each book (unread, reading, completed, abandoned,
  wishlist) and your own rating of one to five stars, with the dates you
  started and finished. Set them on the book's page, or the status of many
  books at once from a selection; lists show them, and the filters narrow
  by them. Every person has their own; nobody else sees them.
- Reading progress is kept per person, for a book's text and its audio
  separately, so that another device picks up where you left off. A device
  that is behind does not overwrite a further position without asking.
  Starting a book marks it as reading, reaching its end as completed, and
  every time you read it to the end is counted. The book's page shows how
  far you are; the readers that write the progress come next.
- A PDF reader in the browser: open a PDF from its book's page with Read.
  It shows the first page without waiting for the whole file, turns pages
  with the buttons, the arrow keys or a page number, zooms, lists the
  contents, and lets you select and copy text. It reopens at the page you
  left, on any device, and offers the page another device got further to.
- An audiobook player in the browser. Listen on a book's page plays its
  files as one recording, whatever the number of parts, from where you left
  off on any device. It lists the chapters, skips 30 seconds, plays faster
  or slower, stops by a sleep timer, and answers the media keys and the
  phone's lock screen. A bar along the bottom keeps playing while you look
  at other pages. Formats your browser cannot play are named, with the files
  to download.
- Statistics: how many pages and minutes a day you read and listen, over
  the last week, month, quarter or year, your totals, the books you
  finished each year (reading one again counts again), and when you
  started and finished which. Page counts of EPUB and MOBI books are
  estimates and marked so. Everyone sees only their own.
- A wishlist. Look a book up with the metadata sources on the Wishlist page
  and wish for it: it gets an entry with its cover and details, marked as
  not in the library yet, and stays out of the library's lists and search.
  When a file of that book is added, by its ISBN or its title and author,
  it joins the wished entry instead of making a second book.
- Collections: shelves you fill by hand and put in your own order. Add a
  book from its page, or many from a selection in the library. A collection
  is private unless you share it; a shared one everyone signed in can look
  at, and each sees only the books of libraries they may see.
- Smart shelves: a shelf defined by rules, such as an author, your rating
  of four stars or more, and unread, combined with "every", "any" and "not"
  and nested in groups. While you build the rules you see how many books
  match. A smart shelf stays current by itself, and shared ones show each
  person what matches for them.
- Read EPUB, MOBI and AZW3 books in the browser, with a table of contents,
  page turning by button, arrow keys or swiping, text size, light, sepia and
  dark pages, and a choice of pages or scrolling. The reader opens where you
  left off, on this device or another. Scripts inside a book never run, and
  a book cannot load anything from the internet.
- Notifications: a bell in the header shows how many you have not read,
  and updates as things happen, without reloading the page. A bulk change
  tells you when it is done. Open the bell to see them, go to what one is
  about, or mark them all as read.
- Search the text of the books: the Search page, or the last entry under
  the search box in the header, finds books whose text holds every word in
  any form, or a phrase in quotes, best first, with the passages that do.
  Typos are repaired when the words as typed find little. Narrow the
  results by library and the library's filters; the address keeps both.
  "Read from here" opens a PDF at the passage's page and an EPUB or MOBI
  at the passage, without moving where you left off until you read on.
- CrossRef is asked for book details beside OpenLibrary: by DOI, by ISBN
  and by title and author. A PDF's DOI is read from its metadata or its
  first pages, so a paper or a scholarly book finds its record. An
  administrator may give a contact e-mail address in Settings, which
  CrossRef serves faster. Installations that chose their sources keep
  their choice; add `crossref` to use it.
- Hardcover is asked for book details beside OpenLibrary once an
  administrator gives a Hardcover API token in Settings (from your
  Hardcover account settings). It knows series and their order, which
  OpenLibrary often does not. Installations that chose their sources keep
  their choice; add `hardcover` to use it.
- GOtome tells you, in the app, of what happens in your libraries: new
  books found by a scan, a book you wished for arriving, details waiting
  for review, new possible duplicates, files that cannot be read, and a
  library that cannot be scanned, each to those who may act on it. A large
  import is one notification, not one per book. Choose what you are told
  of on your profile.
- A book's page suggests similar books: those most like it in what its text
  and its description are about, among the libraries you see. Copies of
  the book and its other editions and translations are not suggested.
- GOtome works out what each book is about, from passages of its text and
  from its title, authors, series, tags and description, so that it can
  suggest similar books. It reads one book at a time in the background,
  using half the processor (`GOTOME_EMBED_THREADS` sets how many threads);
  a library of 50,000 books takes days on a small server. An administrator pauses
  and resumes it, or chooses the model's full-precision weights, in
  Settings. Choosing another model reads every book again.
- The image carries ONNX Runtime for suggesting similar books, which comes
  next. Its language model, 135 MB, will be downloaded into the data volume
  (`models`, or `GOTOME_MODEL_DIR`) the first time it is needed, and checked
  against fixed checksums; `GOTOME_OFFLINE=true` stops the download.
  `gotome embed-check` says whether the runtime loads.
- Replace a copy of a book with the one you keep, from the Duplicates page:
  the other copy's files go to the trash and everyone's status, shelves and
  place read go to the copy kept, the place by how far into the book it
  was. Restoring the file from the trash undoes it. Keeping both copies
  can link them as other editions, translations or related books.
- Merge two copies of a book into one from the Duplicates page: pick the
  book that stays and, where they differ, whose title, authors, series and
  other details it keeps. It gets the other's files and identifiers, and
  everyone keeps what they had with either: status, rating, place read,
  reading history and shelves. Links to the merged book lead to the one
  that stays.
- A trash for files. Editors move a file to the trash from its book's page
  in a library GOtome writes to; it leaves the book and the library's lists
  but stays on disk in the library's `.trash` folder. The Trash page lists
  what is there, says when each goes for good, and restores a file to its
  book. Files are deleted for good after 30 days (an administrator sets how
  many in Settings), or at once by an administrator who confirms it twice.
- A Duplicates page lists books that look like one another, the surest
  first: the same file, the same text with other details, a shared ISBN,
  the same title and author, or text they share ("Persuasion is 97%
  contained in The Complete Austen"). Each pair says why and how sure, and
  the two books can be compared side by side. Editors keep both where they
  are different books. Filter by kind of evidence, score and library.
- The search index rebuilds itself when an upgrade brings a new search
  engine or a new way of reading text, and search keeps answering while it
  does. Editors can have a book's, a library's or every book's text read
  again, and the index rebuilt, from the book, the library and the Jobs
  page, which also shows how much of the text search knows. See
  `docs/search-index.md`.
