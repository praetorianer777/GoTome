# GOtome documentation

For people who run GOtome or use it. Start with **Getting started**.

## Installing and running

- [Getting started](getting-started.md): from nothing to a scanned
  library with Docker Compose, the first administrator, libraries, and a
  reverse proxy with HTTPS.
- [Configuration reference](configuration.md): every `GOTOME_*`
  environment variable and every setting on the Settings page, with its
  default.
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
- [E-readers](devices.md): which way for which device, adding one, and
  sending books.
  - [KOReader and OPDS](koreader.md)
  - [Kobo, and Tolinos on the Kobo-based firmware](tolino-kobo-mode.md)
  - [GOtome for Tolino](../clients/android/README.md), for Tolinos on
    Android firmware
- [Genres](genres.md): where books' genres come from, and measuring
  suggestions before turning them on.
- [Search index](search-index.md): what is rebuilt when, and by hand.
- [Embedding on a GPU](embedding-server.md): `bge-m3` on an Ollama server.
- [How good similar books are](similarity.md): `gotome similar-check`.

## For developers

- [Design](design.md): how the web app looks.
- [Performance at 50,000 books](performance.md).
- [Decision records](decisions/): the search engine, the text store, the
  embedding runtime, the MOBI reader, passage vectors.
