# GOtome documentation

For people who run GOtome or use it. Start with **Getting started**.

## Installing and running

- [Getting started](getting-started.md): from nothing to a scanned
  library with Docker Compose, the first administrator, libraries, and a
  reverse proxy with HTTPS.
- [Configuration reference](configuration.md): every `GOTOME_*`
  environment variable and every setting on the Settings page, with its
  default.
- [Under a sub-path](reverse-proxy-sub-path.md): `https://example.org/gotome/`
  instead of a host name of its own, with Caddy, nginx and Traefik.
- [NAS, arm64 and little memory](platforms.md): what differs on a NAS, a
  Raspberry Pi or a small machine, and the "This machine" panel.
- [Backing up, restoring and upgrading](backup-and-upgrade.md).
- [Troubleshooting](troubleshooting.md): GOtome does not start, signing in,
  files that cannot be read, e-reader fixes, search, similar books on slow
  hardware.

## Using and administering

- [People, roles and libraries](users-and-libraries.md): Reader, Editor
  and Administrator; shared and private libraries; managed and existing
  folders; what GOtome writes.
- [Book details from metadata sources](metadata.md): Open Library,
  CrossRef and Hardcover, their keys, automatic lookups and review.
- [Notifications and new books](notifications.md): what people are told
  of, and following authors, series and feeds.
- [Single sign-on](single-sign-on.md): OpenID Connect with Authentik,
  Authelia, Keycloak and others; roles from groups.
- [The API and API keys](api.md): calling GOtome from scripts with a
  key, its scopes, and the OpenAPI document.
- [The audio API for players](api-audio.md): the stable routes a
  third-party audiobook player streams and saves its position with.
- [Webhooks](webhooks.md): telling other services of events, signed,
  with a log of what was sent.
- [Metrics](metrics.md): a Prometheus endpoint with a scrape example and
  a note on Grafana.
- [E-readers](devices.md): which way for which device, adding one, and
  sending books.
  - [KOReader and OPDS](koreader.md)
  - [Kobo, and Tolinos on the Kobo-based firmware](tolino-kobo-mode.md)
  - [GOtome for Tolino](../clients/android/README.md), for Tolinos on
    Android firmware
- [On a phone and offline](offline.md): installing the app, keeping books
  in the browser, and what is stored.
- [Highlights and notes](highlights.md): making them in the readers,
  bringing them from KOReader, exporting them.
- [Send to Kindle](kindle.md): e-mailing EPUBs to a Kindle, the mail server
  and the sender Amazon has to approve.
- [Your reading history and importing it](importing-reading-history.md):
  books you read but do not own, notes, and the Goodreads and StoryGraph
  import.
- [Moving from Calibre](moving-from-calibre.md): reading a Calibre library's
  `metadata.db` in place, what is taken from it, and running it again.
- [Organising a library's files](organising-files.md): renaming and
  sorting files into folders by a pattern, with a preview and undo.
- [Resolving duplicates by themselves](duplicates.md): the classes of
  duplicate pairs the rules merge or link, dry runs, undo, and measuring
  them with `gotome duplicate-check`.
- [Discover](discover.md): books beyond your libraries from your taste,
  what the metadata sources are asked, and how to switch it on.
- [Genres](genres.md): where books' genres come from, and measuring
  suggestions before turning them on.
- [Accessibility](accessibility.md): WCAG 2.2 AA, what is checked, what is
  not met, and how to report a barrier.
- [Search index](search-index.md): what is rebuilt when, and by hand.
- [Embedding on a GPU](embedding-server.md): `bge-m3` on an Ollama server.
- [How good similar books are](similarity.md): `gotome similar-check`.

## For developers

- [Design](design.md): how the web app looks.
- [Performance at 50,000 books](performance.md), and `gotome perf-check` for
  timing the views on your own library.
- [The demo](demo.md): the Pages build, its sample data and the books it fetches.
- [Decision records](decisions/): the search engine, the text store, the
  embedding runtime, the MOBI reader, passage vectors, genre detection, the
  modern genre corpus.
