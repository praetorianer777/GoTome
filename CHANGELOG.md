# Changelog

What changes for someone who runs or uses GOtome. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed

- On a phone, the wishlist's lookup results no longer squeeze their text into a narrow column: the buttons now sit below the text.

## [0.15.0] - 2026-11-10

### Added

- A new page, Changes, lists what was merged, replaced or cleaned up, run by
  run, with each book's details before and after. A run, or one change of
  it, can be undone: the merged book comes back with its files, shelves and
  reading, and files moved to the trash return to their book. A change to a
  book that was edited since is left as it is and reported.
- The match review, the genre review and the clean-up page show a book beside
  the list when you click it (a full-screen view on a phone): cover, names,
  description, files, tags, a link to its page and "Look inside", which opens
  the book in its reader without saving a place, a reading session or a
  status. The open book is in the address, so back closes it.
- A genre suggestion shows the book's description (folded to a few lines) and
  tags in its row.

### Changed

- Comparing a found match with a book (review and "Find details online") now
  lists every field side by side, not only the ones that differ: equal fields
  are plain, differing ones highlighted and ticked as before, empty ones
  marked. On a phone the two sides are stacked.
- On a wide window each clean-up suggestion is one row, with its buttons at
  the right end, instead of three or four lines.
- On the clean-up page a suggestion goes away as soon as you click it, and
  comes back with the error if the server refuses; applying or dismissing one
  no longer works out every suggestion of its kind first.

### Fixed

- The list of authors no longer runs past a phone's screen when a name is
  very long.
- On the duplicates page the two "Keep this" buttons of a pair stand at the
  same height whatever the books' titles say, and the books' details line up.

## [0.14.1] - 2026-11-10

### Fixed

- A library's last scan of a single file says "one file", not "1 files".
- `gotome similar-check -recommend` says when nobody has enough books rated
  4 or 5 stars to measure, instead of printing a table of zeros.

## [0.14.0] - 2026-11-10

### Added

- Libraries watch their folders: files put into (or taken out of) a library's
  folder are found by a scan about a minute later, without pressing "Scan
  now". Network shares are checked every minute instead, and the overview
  tells when the kernel's limit of watches is reached. It can be turned off
  per library.
- Send to Kindle: when an administrator has set up a mail server (Settings,
  Mail), a book's EPUB goes to your Kindle by e-mail, from the book's page or
  for books selected in a list. Add your Kindle address on your profile and
  approve the sender address in your Amazon account. Only EPUB files up to
  50 MB are sent, unchanged; each send is a job whose outcome is on the book's
  page and in a notification. See `docs/kindle.md`.
- Books you read but the library does not hold (most of an imported Goodreads
  or StoryGraph history) now count in what "For you" learns from your taste,
  and are never recommended themselves. See `docs/similarity.md`.
- Statistics count the books completed with no finish date, such as those an
  imported history gave none, apart from the years ("Completed without a
  date"), without inventing a date for them.
- Books an imported Goodreads or StoryGraph history adds that the library
  does not hold are looked up by their ISBN like a new file is, so they get
  a cover and a description. See `docs/importing-reading-history.md`.

- The audiobook player's sleep timer fades the volume down before it stops,
  can be extended by 15 minutes (a button, or shaking the phone where the
  browser allows it, also just after it stopped), and can start by itself
  when you play a book between hours you choose. A volume boost of up to
  twice, with a limiter against clipping, helps with quiet recordings. The
  night hours and the boost are kept per browser.
- A queue for audiobooks: "Play next" and "Add to queue" on a book's page,
  "Add to the queue" for books selected in a list and "Queue the rest of
  this series" on a series' page. The next book starts by itself when one
  ends, from where you left it. The queue is yours, kept on the server, so
  it is the same in every browser, and is listed under the player, where you
  can reorder or clear it.
- Two recordings of one audiobook by different narrators are no longer
  suggested as duplicates to merge: the Duplicates page says "another
  narrator" and links them as other recordings, which each book's page then
  lists with their narrators. The library can be filtered by narrator.
- An ebook and its audiobook of the same title and author, which a scan
  makes two books because they lie in different folders, are shown on the
  Duplicates page as such. A library can join them by itself (Libraries
  page, "Make one book of an ebook and its audiobook ..."); a book joined
  so says so on its page and can be split again. Between two libraries
  nothing is merged, as a file stays in its library's folder: the pair can
  be linked, and each book's page lists the other under "Also as".
- "For you": a row in the library's strip, and a page with more, recommend
  unread books of the libraries you see that are near what you finished or
  rated well, each with the books it is "because you liked". Someone who
  reads crime and fantasy gets both. "Not interested" leaves a book out
  for good. It needs three finished or rated books and the embedding of
  similar books; nothing is sent anywhere. `gotome similar-check -recommend`
  measures it on your ratings (`docs/similarity.md`).
- Your reading history now includes books the library does not hold: on
  Wishlist, "Add as read" (with stars and the day you finished it) and
  "Currently reading" sit beside "Wish for it", and "Your reading" lists
  everything you read with filters by year, status and stars and a search.
  Every book page has a private note. See `docs/importing-reading-history.md`.
- Profile, "Import reading history": bring in a Goodreads or StoryGraph
  export (a CSV file). You see a preview first, pick the right book for
  rows that could be several, and the import sets status, stars, finish
  dates, notes and collections for custom shelves; what you set already
  stays, and importing the same file again changes nothing.
- Administrators are told when a new GOtome release is out: a line in the
  footer, a notification once per version (you can switch it off on the
  profile), and the latest version on the overview page with a link to the
  upgrade guide. GOtome asks GitHub once a day, with no account and no
  token; the setting "Check for new releases" and `GOTOME_OFFLINE=true`
  turn it off. It never updates itself.
- Read aloud in the web reader for EPUB, MOBI and AZW3 books: a "Read aloud"
  button (or the R key) reads from the top of the page with the browser's own
  voices, highlights the sentence being read and turns pages as it goes. The
  voice (the book's language picks the first) and the speed are chosen under
  "Display" and kept per browser. Nothing leaves the browser; the button is
  left out where the browser has no voices, and PDFs are not read aloud.

### Fixed

- On a phone every button, field and link is at least 44 pixels high with
  8 pixels between neighbours, text fields are 16 pixels so iOS no longer
  zooms into them, and bars at the screen's edge keep clear of the notch and
  the home indicator.
- On a 320 pixel wide screen the settings page and the library and search
  pages with several libraries no longer slide sideways, the search page no
  longer jumps when its first results arrive, and small links and the rating
  stars are at least 24 pixels high. The profile no longer shows three
  filled buttons at once.

## [0.13.0] - 2026-11-09

### Added

- API keys on the profile, for scripts and other programs that use GOtome
  without a browser: give a key a name, the scopes it may use (read the
  library, reading status and progress, upload, edit metadata, administer)
  and an optional expiry. It is shown once, sent as `Authorization:
  Bearer`, and never does more than your role allows. The list shows when
  each was last used; revoking one stops it at once. See `docs/api.md`.
- `gotome perf-check` times the main views on your own library and prints a
  table that names none of its books, with the memory in use. It only reads.

### Changed

- An upgrade with long database migrations no longer shows the app as
  `unhealthy` or makes `docker compose up --wait` fail: GOtome listens at
  once, `/readyz` says `migrating` (and how far it is) until they are done,
  and Docker's health check allows ten minutes for that. The monitoring
  addresses are described in the troubleshooting guide.
- `/readyz` now answers 200 only when the database, the migrations, the
  search index, the job queue and the data folder all work, and otherwise
  503 naming the parts that fail (and nothing else, as it is public).
  Administrators see how each part is doing, and what is degraded without
  stopping GOtome, on the Overview page and at `/api/v1/system/checks`, and
  are told when a part has failed for ten minutes.

### Fixed

- The dark theme's field and button borders are a little lighter, so they
  stay easy to see over the glass panels, and the library behind the panels
  shows more at night: only its lamps are held down, not the whole picture.
- The "GO" of the small wordmark in the top bar reads cyan again instead of
  teal, and the gopher's pictures no longer show a white patch between its
  head and its headphone band; the sleeping gopher's speech bubble has a "z".

## [0.12.0] - 2026-11-09

### Added

- A strip at the top of the library: "Continue reading" with the books you
  are in the middle of, their progress and one tap to where you were, and
  "New in the library" with the newest books. It folds away and stays
  folded, starts folded on a phone, and is left out when you search or
  filter.

### Changed

- The name in the top bar and on the sign-in and setup pages is a new
  wordmark: an illuminated "GO" and "tome" in gold lettering, one for each
  colour theme.
- Books in the library's grid, on author and series pages and in "similar
  books" are shown as rounded cards, with a round badge on the cover's
  corner saying whether the book is an ebook, an audiobook or both.
- The dark theme's colours follow the owner's draft more closely: a deep
  midnight blue with a faint warm glow at the corners, translucent navy
  panels with soft borders, a brighter cyan accent, gold page titles, and
  a lighter blue-grey pill for the current tab and menu entry. The light
  theme keeps its parchment, with muted text and borders slightly bluer.
- The app now carries GOtome's own artwork: the library at night or by day
  shows through the glass-like panels, the gopher stands at the sidebar's
  foot and in the page corner, sleeps in empty lists, searches where nothing
  matched, looks confused on errors and missing pages, reads while a page
  loads, and cheers with a few stars when you mark a book finished. The
  medium badges on covers are the new round badges, and covers lift with a
  soft glow under the pointer in the dark theme. Nothing moves with
  "reduce motion" set; panels are opaque with "reduce transparency" and on
  phones. The sign-in and setup pages open with the gopher reading in its
  library, by night or by day with the colours.

## [0.11.0] - 2026-11-09

### Added

- `gotome match-check` measures how often a lookup by title and author
  alone finds the right book, on a sample of your books that have an
  ISBN: per source and language, with the scores of right and wrong
  first candidates, to choose `metadata.matchThreshold` by
  ([metadata.md](docs/metadata.md)).
- An embedding model no longer in use has its downloaded files, its
  vectors and its passage vectors removed once it has not been chosen for
  a day (the new setting "Days an unused model is kept",
  `embedding.removeUnusedAfterDays`). The model list on the settings page
  shows what each model takes on disk and in the database, and "Remove
  now" frees one not in use at once.
- Enter in the header's search, with no suggestion chosen, lists every
  book the words find, like the library: sorting, covers or a list,
  filters, selection and bulk changes. Its address keeps the words, so
  back and reload return to it. A suggestion chosen with the arrow keys
  still opens that book, author or series.
- Clean up suggests a series for books without one whose titles name it,
  such as "Rain Wild Chronicles 01 - Drachenhüter" or "Drachenhüter (Rain
  Wild Chronicles #1)", where two books of a library name it or it is a
  series already. Applying it puts the books into the series at the
  places their titles say and takes the series off their titles.

### Changed

- The app has a new look, after the owner's draft
  ([design.md](docs/design.md)): the top bar carries the wordmark, the
  sections (Library, Wishlist, New books, Statistics), a rounded search and
  the bell and account menu; the sidebar is a panel grouped as Ebooks and
  Audiobooks, with "All ebooks" and "All audiobooks" opening the library
  filtered to one medium; pages sit on rounded panels over a dimmed
  backdrop in the dark theme, and the light theme has the same layout.
  Below 1024 px the menu button opens all of it as the drawer.
- The web app opens faster, the first time and after a hard refresh: its
  files are sent compressed (brotli or gzip), pages other than the
  library, a book and sign-in load when first visited, and large API
  answers are compressed as well. The first load moves about 170 KB
  instead of 800 KB.
- The reading status "Wishlist" is called "Want to read" for a book the
  library holds, and stays "Wishlist" for one it does not hold yet. The
  Wishlist page lists the two apart, as "Want to read" and "Not in the
  library yet".
- The settings page is grouped in sections (sign-in, book details,
  similar books and search, genres, trash) with a list to jump to each,
  two columns where the screen is wide enough, and shorter
  descriptions. A setting of a few values is chosen rather than typed:
  on and off is a switch, the genre mode and the role of everyone else
  are lists, and the sources to ask a box each. The server refuses a
  value that is not one of them, such as an unknown source.
- Smart shelf rules pick formats and stars from a list instead of
  taking them typed.
- **Review** and **Find details online** tick the fields worth taking:
  those the book lacks, a title from the file name, and, from a candidate
  of the same ISBN, those no person set. A field whose value would
  replace your edit, the file's, or text in another language starts
  unticked, each says why, and values that would be replaced are
  highlighted. Accepting a match takes what is ticked.
- The header's search suggestions are wider than the box where there is
  room, and show a title over two lines instead of cutting it.
- The duplicates page shows the two books of a pair side by side as
  cards, with their covers, files with size and pages, e-reader
  findings, your reading, library and added date, and what differs
  marked; stacked on a phone. Each card has one **Keep this one**, which
  says what happens to the other book. In a library GOtome does not
  write to, it merges the other book in with its files instead of
  trashing them.

### Fixed

- A book's page keeps long titles, author lists, series, publishers,
  identifiers, tags and file names inside their boxes at every width:
  they wrap, and words too long for a line break, instead of running
  over the details card and off the screen. The details card is a little
  wider, and lists each identifier on a line of its own.
- Keeping one book of a pair, moving a file to the trash, restoring it
  or fixing it in a folder GOtome may not write to (a `.trash` or an
  author's folder owned by another user, a read-only mount) failed with
  a server error and did nothing visible. It now says which folder
  cannot be written and why, right at the button, and marks the library
  as one whose folder cannot be written.

## [0.10.0] - 2026-11-06

### Changed

- More tags give genres: tags a store marks, such as "Science Fiction
  (st)" or "Frei - Fantasy", count as the tag beneath, and "Star Trek",
  "Darkover", "Geschichtswissenschaft", "Erzählung" and "Zeitgenössisch"
  are known spellings. `gotome genre-check` no longer lists tags that
  rightly give no genre, such as "Roman", "eBook", publishers and
  countries, among those to map.

### Fixed

- An author or series followed while GOtome was looking up the others is
  looked up right after, instead of only at the next hourly round.
- A book's page shows the status reading gave it straight away: going
  back from the reader or the player after reading to the end said
  "Unread" or "Reading" until the page was loaded again.
- Searching by description could rank passages by another passage's
  vector after GOtome stopped while it was compacting the passage store.
  The store now switches to its compacted files in one step, and one
  left half switched by an earlier version is started over at the next
  start, its passages embedded again.
- A feed whose items cannot be recorded, such as one with a very long
  link, is reported as failed and tried again later, instead of keeping
  every other feed from being fetched.
- Changing a feed's keywords or authors applies them to the items already
  fetched, not only to new ones.
- A series' page lists the books the sources announce for it however they
  spell its name, such as "the expanse" for "The Expanse".
- A series page no longer hangs on a book at an absurd place in its
  series, such as one an EPUB gives as 1e12. A position above 100,000, or
  one that is not a number, is refused in an edit and left out when a file
  or a metadata source gives it.
- Edits made while a writable library's folder could not be written are
  written into its EPUBs once it can be again, at the next scan or when
  the library is made writable, instead of staying in the catalogue only.

## [0.9.0] - 2026-11-06

### Added

- `gotome reset-password <user name>` sets a new password from the shell
  when no administrator can sign in, and signs the account out; `-enable`
  and `-admin` also enable it and make it an administrator
  ([docs/troubleshooting.md](docs/troubleshooting.md#signing-in-and-changes)).
- `GOTOME_TRUSTED_PROXIES` names the reverse proxies in front of GOtome.
  From those, the client's address is taken from `X-Forwarded-For`, so the
  limits on failed sign-ins, device requests and single sign-on starts
  count each person apart instead of everyone behind the proxy together;
  once it is set, `X-Forwarded-Proto` is believed only from them
  ([docs/getting-started.md](docs/getting-started.md#trust-the-proxy)).
- The Libraries page lists a private library's members and lets an
  administrator add and remove them, without the API.

### Fixed

- **GOtome may change files here** is refused, with the reason and the
  folder's path, on a folder GOtome cannot write to, such as one mounted
  with `:ro`. A writable library whose folder turns read-only later is
  shown so on the Libraries page and told once to administrators, instead
  of failing file by file.
- `GOTOME_EMBED_THREADS` set in `.env` now reaches the app; it needed an
  override file before ([docs/configuration.md](docs/configuration.md)).
- Background work no longer can stop for good with "completion subscriber
  received a job that wasn't finalized" on a heavily loaded machine: queueing
  a job that was already running kept that job from being marked finished
  until the change that queued it was saved.

## [0.8.0] - 2026-11-05

### Changed

- More tags give a genre: romance imprints (Julia, Romana, Baccara, Bianca,
  Tiffany, Historical RomanHefte), Jugend, Mord, Historisch, Historischer
  Krimi, horror series (Dorian Hunter, Dämonenkiller, Vampir Horror Roman),
  Rettungskreuzer Ikarus, Scheibenwelt and Sex.
- Genre suggestions from the nearest books need 40% of them to agree
  instead of half, and `combined` now follows them, using the comparison
  with the genres' descriptions only for a book whose neighbours have no
  genres: on a real library that comparison was no better than guessing
  ([docs/genres.md](docs/genres.md)). Use `neighbours`.

### Security

- An identity provider's signing keys are read with go-jose 4.1.6, which
  no longer can be made to spend unbounded time on a crafted RSA key.

## [0.7.0] - 2026-11-05

### Added

- A page for each author, with all their books under every spelling of
  their name, series by series, and a page for each series, in its order,
  saying how many you have read, which is next, which are missing, and
  offering the books the sources list that the library lacks for the
  wishlist.
- Authors and series are links wherever a book is shown, and the quick
  search offers them.
- Authors and Series in the navigation list every author and series from
  A to Z, with how many books each has.
- Documentation for installing and running GOtome
  ([docs/README.md](docs/README.md)): getting started with a reverse
  proxy, every environment variable and setting, NAS, arm64 and small
  machines, roles and libraries, metadata sources, single sign-on,
  e-readers, and troubleshooting.
- New books can come from publishers' and authors' RSS and Atom feeds: on
  New books, follow a feed's address with words or authors an item must
  name, and you are told once of each new item that does. A feed is read
  hourly, asking only for what changed; one that cannot be read says why
  and is tried less often, then stops until you try it again. Feeds on
  GOtome's own network are refused.
- Genres from a fixed list of 44, in English and German: books get the
  genres their tags mean ("Krimi", "Kriminalroman" and
  "Belletristik/Kriminalromane" are all Crime), and GOtome can suggest more
  from what a book is about or from the books most like it, for review
  under Genres. The library's filter and smart shelves find books by
  genre. Nothing is written until an administrator sets "Genres for books"
  (`genres.suggest`); `gotome genre-check` measures the suggestions on a
  library first. A book's tags are never changed.
- The search by description can find books by their passages: with the
  new setting "A vector for every passage" (`embedding.passages`, off by
  default) every passage of a book's text gets a vector of its own, so
  that a description of a scene finds the book it happens in. It reads
  every book again, which on a large library takes days of the CPU, and
  keeps about 1.5 GB per 50,000 books under `/data/passages`, outside the
  database and the backups. The jobs page shows how far it is.
- The e-reader check also finds chapter files that are not well-formed
  XHTML, which some readers such as the Tolino's refuse, and tables of
  contents with entries that lead nowhere, and repairs both: a chapter file
  is written again only where its text stays the same, and an entry is
  taken out only when it leads nowhere, or pointed at its chapter's start
  when only the place in it is gone. EPUBs checked before are checked again
  once.

### Changed

- The app image is far smaller, about 260 MB instead of 865 MB: it carries
  a small FFmpeg of its own that only reads audio files' tags, chapters
  and covers, in place of Debian's whole FFmpeg. Fewer libraries are within
  reach of a hostile file.

### Fixed

- A file asked to be read again just as its last read finished is read
  again, instead of staying "Not read yet" until the next scan.

## [0.6.0] - 2026-10-15

### Changed

- The web app has a new look taken from GOtome's artwork: a midnight-blue
  dark theme with the gopher's cyan, a parchment light theme, and serif
  headings. The navigation is a sidebar in groups (the catalogue's and the
  administration's fold away) and, on phones and tablets, a menu that opens
  from the left.
- An account menu at the top right holds Profile, E-readers, the language,
  the colours and Sign out; Profile is also in the navigation, so it is
  easy to find. Someone single sign-on does not know yet is taken to their
  profile once they sign in with their password.
- The library shows its covers larger, like books on a shelf, with the
  book being read marked in candlelight; the filters stay in view while
  the list scrolls, and the first page loads behind placeholders of the
  covers to come.
- A book's page leads with its cover and title, with Read (or Listen) as
  its first button, and gives the details a box of their own beside the
  files.
- Every other page follows the same design: one kind of page heading,
  buttons and toggles in the accent colour, placeholders while lists load,
  and progress bars and checkboxes in the accent.

### Added

- The web app's pages end in a footer that says which version of GOtome runs,
  linked to its release notes, and, when GOtome was updated while the page
  was open, that it should be reloaded.
- The overview tells administrators when the machine is too small for
  what runs on it: Postgres's memory settings (which travel with the
  database's volume, sized for the machine it was first made on) with the
  app's and the embedding model's, more parallel workers than CPUs, or a
  nearly full disk (under 10 GB or 10% free). Each warning comes with the
  commands that make the settings fit, and is logged at start and hourly
  while it stands. `docs/backup-and-upgrade.md` says how to move to another
  machine and how much room an installation needs.

## [0.5.0] - 2026-10-15

### Added

- The EPUB and PDF readers and the audiobook player can be used from the
  keyboard alone: pages turn with the arrows, Page Up and Page Down and the
  space bar; C opens the contents, F a search in the book, D the display
  settings; the player plays and pauses with the space bar or K, seeks with
  the arrows and changes chapter with Shift and the arrows. The "Keys" button,
  or ?, lists them all.
- The EPUB and PDF readers search the book they show.
- What makes an EPUB slow on an e-reader can be fixed, in libraries
  GOtome writes to: each finding on the book's page has its button, and
  "Fix for e-readers" does it for every selected book. A long chapter file
  is split at its chapters, images are scaled down to 1600 pixels, the
  embedded fonts are removed, and files the EPUB lists but lacks are taken
  off its list. The file as it was goes into the trash, from where it can
  be restored beside the fixed one; where you were reading is kept, and
  e-readers that had the book are sent it again.

### Changed

- GOtome no longer keeps a copy of the books' text: full-text search runs on
  an index of its own on the data volume (`search/`), and snippets are read
  from the books' files. The database shrinks by most of its size (for
  48,000 EPUBs from about 28 GB to about 3.5 GB, beside an index of about
  7.6 GB); the space is freed during the first start after the update,
  which takes a few minutes longer on a large library. That start then
  reads every book once more in the background to build the index ("Read
  the books' text into the search index" on the jobs page), which takes
  about as long as reading the library did the first time, an hour or two
  for 48,000 EPUBs; until a book is read, full-text search does not find
  it. Backups leave the index out; a restore builds it again from the
  books. The database no longer needs ParadeDB's `pg_search`. This
  update cannot be undone: back up first (`deploy/backup.sh`), as going
  back to 0.4 means restoring that backup on the 0.4 image.
- Screen readers are told of new notifications, of each finished upload and
  of the page a PDF turns to; panels and dialogs take the focus when they
  open and give it back when they close. The focus ring, grey text on the
  dark colours and the empty rating stars are easier to see.

### Security

- An upload whose content is not the format its name says, such as a web
  page saved as a PDF, is refused with the reason, and a file like it in a
  library folder is marked as unreadable without being read.
- EPUBs built to hurt their reader, such as zip bombs or archives whose
  entries point outside them or stand twice, are marked as unreadable.

## [0.4.0] - 2026-10-14

### Added

- Each e-reader on your profile has a page of its own with the books sent
  to it: waiting, announced to a Kobo reader, or on the device, with their
  dates. A book still waiting, or on a Kobo reader, can be taken off from
  there; a Kobo reader removes it at its next sync.
- Books whose files a scan no longer found in the library folder can be
  found: the library's filters offer "Files", with books that miss some
  and books that have none left; the grid and the table mark a book none
  of whose files is left; and the overview counts them, with a link to the
  list, after a move or a mount that went wrong.
- The settings page offers the embedding model as a choice from a list,
  with each model's download size, roughly how much memory it needs on the
  CPU, whether it can run here and how many books already have its
  vectors. Switching to one that lacks most of them asks first, as every
  book is then embedded again. The jobs page links to it.
- GOtome speaks German. It follows the browser's language, and each
  browser can choose another on the profile. Dates and numbers are written
  the way the language writes them. Messages the server words about a
  particular case, such as why a field was refused, stay in English.
- A failure on the server shows its request ID, by which an administrator
  finds it in the log.
- EPUBs that may turn pages slowly on an e-reader are found: a chapter file
  over about 300 KB, embedded fonts over 1 MB, an image over 4 megapixels
  or 1 MB, or files the EPUB lists but lacks. The book's page says which
  of these a file has, the library's filters offer "On e-readers" (also
  as a rule of a smart shelf), and the overview counts such books.
  Files read before are measured in the background.

### Fixed

- Deleting a library while its books are being read, moved to the trash or
  merged no longer fails now and then with an error.
- The GOtome book on a Tolino opens GOtome for Tolino again while it is
  signed out, after a restart or an update: it then shows the setup screen.
- bge-m3 runs in GOtome on the CPU when no embedding server is set, as
  0.2.0 said it would; until now it waited for a server.
- Changes to a book are written into its EPUB file again when the file's
  archive lists its folders as entries of their own, as many tools write
  them; before, writing failed with "zip: write to directory".

### Security

- Every request GOtome makes to another server refuses addresses inside
  your network, also after a redirect or when a name resolves to one,
  except the identity provider and the embedding server you set up. An
  identity provider whose discovery names an endpoint on another address
  inside the network is refused. The embedding model's download no longer
  goes through a proxy from `HTTPS_PROXY`.
- Every answer carries headers that keep browsers from guessing a file's
  type, from sending GOtome's addresses to other sites and from framing
  its pages, and everything outside the web app is sandboxed.

## [0.3.1] - 2026-10-14

### Fixed

- A Kobo reader, or a Tolino in Kobo mode, now shows the books it is sent:
  newer firmware dropped them without a word. Readers synced before are
  sent their books again.

## [0.3.0] - 2026-10-14

### Added

- Kobo readers, and Tolinos switched to Kobo mode, sync with GOtome over
  their own sync: books sent to the device arrive with covers and details,
  and the reading place goes both ways. Add the reader as a device on your
  profile and paste the line it shows into the reader's config; see
  `docs/tolino-kobo-mode.md`.
- `docs/tolino-kobo-mode.md` explains how a Tolino on the Kobo-based firmware
  (Shine 5, Shine Color, Vision Color) is switched to Kobo mode, where it
  syncs with a server named in its config, and what was found on a Shine 5
  doing it: what wipes the reader, the Kobo account it needs, and the
  firmware updates offered afterwards.

## [0.2.0] - 2026-10-14

### Added

- GOtome runs bge-m3 itself on the CPU when no embedding server is set: a
  library embedded on a machine with a GPU keeps its vectors on a machine
  without one, and new books and searches by description go on with the
  same model. The first use downloads 2.3 GB. `gotome embed-check -model
  bge-m3 -against-stored 50` checks the CPU's vectors against the stored
  ones; see `docs/embedding-server.md`.
- GOtome for Tolino signs in without a token typed on the e-reader: give it
  the address and your user name, tap **Request access**, and accept the
  Tolino on your profile under E-readers when it shows the same code. The
  device token stays as the other way.
- GOtome for Tolino shows its version on its settings page, and speaks
  English or German as chosen there, English unless changed, whatever
  language the Tolino is set to.
- `deploy/.env.example` lists every setting of the compose deployment with
  its default; copied to `.env`, it pins the image to a version you choose.

## [0.1.1] - 2026-10-14

### Added

- `make android-install FROM=release` puts the latest release of GOtome for
  Tolino on a connected Tolino (`TAG=vX.Y.Z` for another), with the same
  grants as a build from the checkout; that release then updates itself.
- GOtome for Tolino's settings page has a **Check for updates** button,
  which looks for a new release at once instead of waiting for the daily
  check, and says when the app is up to date.

## [0.1.0] - 2026-10-14

### Fixed

- The database no longer restarts itself, dropping every connection, when
  the machine is busy enough for its health check to time out. Take the
  `init: true` of the `db` service into your compose file.

### Changed

- The duplicates page lists a book whose text another one holds, such as a
  story and its collection or an episode and its omnibus, under its own
  heading, "Contained in", instead of among the copies to decide about.

- GOtome is licensed under the Apache License 2.0 instead of the AGPL-3.0.
- Similar books show at most two books by one author and two from one
  series, and no copy of the book: on a library of 50,000 books, a book's
  similar books now have eight authors on average instead of five, and
  fewer than one in five is by its own author instead of more than half.

### Added

- GOtome for Tolino comes with each release as a signed APK, and an
  installed release offers the next one on the Tolino and installs it after
  one confirmation (`clients/android/README.md`).
- A GOtome plugin for KOReader, downloaded from the profile page, fetches
  the books sent to the device by itself whenever Wi-Fi comes up, into a
  folder of your choosing (`docs/koreader.md`).
- Books are sent to an e-reader from their page or a selection in the
  library: they wait in the catalogue's "Sent to this device" on the
  device until it downloads them, and the book's page says since when the
  device has it.
- KOReader's own progress sync works with GOtome as its server: a place
  read in KOReader opens in the web reader, and one from the web reader is
  offered in KOReader, at its chapter for an EPUB and its page for a PDF
  (`docs/koreader.md`).
- GOtome for Tolino, an app for Tolino e-readers on firmware 16, installed
  over USB (`clients/android/README.md`) and signed in with a device token.
  Books sent to the Tolino arrive on the reader by themselves, a store on
  the reader browses and searches your library and puts books on it, and
  how far you read in the Tolino's own reader shows up in GOtome. Opening a book you read further
  elsewhere, the Tolino offers to jump to that page.
- E-readers with KOReader, or any app that reads OPDS catalogues, browse
  the library by newest, author, series and shelf, search it and download
  books, signed in with a token made for the device on the profile page
  (`docs/koreader.md`).
- An overview page shows what the libraries hold (books, files by format,
  size, pages, audiobook hours, authors, series, languages, books added per
  month, and books without a cover, an author or text) and, for editors,
  how far the background work is, with its pace and the time left: files
  read, texts indexed, books worked out for similar books, lookups and
  duplicate checks. One library or all of them.
- The jobs page shows how far GOtome has got working out what books are
  about, and lets an administrator pause it and start it again with one
  button, for instance to have the GPU back for a while.
- Books can be embedded on a GPU: with an Ollama server set as the
  embedding server, the larger multilingual model bge-m3 works out what
  books are about, about two and a half times as fast as the CPU did with
  the smaller one (`docs/embedding-server.md`). Switching models keeps the
  vectors of the one before, so going back is at once.
- Search by description: on the search page, describe what you want to
  read ("Ein Kommissar ermittelt in Venedig", "a family saga over three
  generations") and get the books whose text and description come
  nearest, best first. It needs the embedding model and the books'
  vectors; while they are not there, the page says so.
- `gotome similar-check` measures how good the similar books are on your
  own library: how many are by the book's own author or from its series,
  and how many are copies of it (`docs/similarity.md`).
- A "Clean up" page suggests fixes for how books are described, each to
  apply or leave with one click, or all of a kind at once: one author under
  several spellings ("Goldstein, Barbara" and "Barbara Goldstein") made
  one, values such as "authors_sort" or "Unknown" taken off as authors, an
  author who is the name of their books' series, and what shops add to
  titles, such as "(German Edition)" or ": Roman". A fix is your edit:
  fields you locked stay as they are.
- Duplicates are found, and wishes fulfilled, whichever way round an
  author's name is written; editing a book's title or authors looks for
  its duplicates again.
- Follow authors and series for their new books: from a book's page, or by
  name on the new "New books" page, which lists what is announced and what
  came out in the last year. GOtome asks Hardcover (with its token) and
  Open Library once a day and tells you when a book is announced and when
  it comes out, unless a library you see has it already. Following an
  author does not tell you of their older books. Both kinds of
  notification can be switched off on your profile.
- Scrolling far into a large library stays smooth: the page keeps only the
  books near the window, however many it has loaded, and going back from
  a book returns to the same place in the list. In Safari the list no
  longer stops loading after its second page.
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
