<p align="center">
  <img src="assets/branding/banner.jpg" alt="GOtome: a gopher with headphones reading a glowing book" width="480">
</p>

# GOtome

Self-hosted library manager for ebooks, PDFs and audiobooks.

Read ebooks and PDFs in the browser, listen to audiobooks, find books by a
word of their text, send them to your e-reader, and share the library with
your household, each person with their own reading state.

- Libraries of folders you already have, or of uploads GOtome arranges
- Details from Open Library, CrossRef and Hardcover; editing, writing them
  back into EPUBs, bulk changes and a review queue
- Exact and near-duplicate detection, with merging and replacing
- Filters, smart shelves, collections, a wishlist; typo-tolerant full-text
  search inside the books' text, and search by description
- In-browser EPUB, Kindle and PDF reader and audiobook player, with the
  reading place kept in step across devices; reading statistics
- Similar books, worked out from what books are about
- Following authors and series for new books, with notifications
- KOReader (OPDS and progress sync), Kobo, and Tolino
- Readers, editors and administrators; shared and private libraries;
  single sign-on through OpenID Connect

## Stack

Go backend, React and Tailwind CSS frontend, PostgreSQL. A deployment is two
containers: the app and the database.

## Running it

Each release publishes the image for amd64 and arm64 as
`ghcr.io/praetorianer777/gotome`. Everything a deployment needs is the folder
`deploy`:

```bash
git clone https://github.com/praetorianer777/GoTome.git && cd GoTome/deploy
cp .env.example .env   # optional: pin the version, change the port
docker compose up -d
```

Then open <http://localhost:8080> and create the first account. A short-lived
`init` container gives the installation its own random database password and
encryption key on the first start and keeps them in the `secrets` volume; nobody
has to choose or type them. The key encrypts what is stored for other services,
such as API tokens: back up the `secrets` volume together with the database, or
those have to be entered again.

The [documentation](docs/README.md) takes it from there:
[getting started](docs/getting-started.md) (libraries, a reverse proxy),
the [configuration reference](docs/configuration.md),
[backing up and upgrading](docs/backup-and-upgrade.md),
[e-readers](docs/devices.md) and [troubleshooting](docs/troubleshooting.md).

## Development

Every change belongs to an issue and lives on a branch `<type>/<issue>-<slug>`;
see [CLAUDE.md](CLAUDE.md). `./run-tests.sh` is the gate that must pass before a push.

## Licence

[Apache 2.0](LICENSE)

The artwork is based on the Go gopher, designed by
[Renée French](https://go.dev/blog/gopher) and licensed under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
