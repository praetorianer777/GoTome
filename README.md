<p align="center">
  <img src="assets/branding/banner.jpg" alt="GOtome: a gopher with headphones reading a glowing book" width="480">
</p>

# GOtome

Self-hosted library manager for ebooks, PDFs and audiobooks.

**Status: early development.** Nothing is usable yet. The plan is tracked as
[issues](https://github.com/praetorianer777/GoTome/issues) grouped into
[milestones](https://github.com/praetorianer777/GoTome/milestones).

## Planned

- Exact and near-duplicate detection (file hashes, ISBN, title and author, text
  overlap via MinHash/LSH) with a merge and replace dashboard
- Metadata from OpenLibrary, Google Books, Hardcover and CrossRef; editing, EPUB
  write-back, bulk operations and background matching
- Filtering, typo-tolerant and full-text search inside book text
- Per-user reading state, synced progress, statistics
- In-browser EPUB/PDF reader and audiobook player
- Smart shelves defined by rules
- Release tracking for authors and series with notifications (Discord, Telegram,
  ntfy, Gotify, in-app)
- Content-based "similar books"
- Multi-user with roles (Admin, Editor, Reader), private and shared libraries,
  optional OIDC

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

`deploy/.env.example` lists what can be changed, each with its default. With
`GOTOME_IMAGE` pinned to a version you choose when to upgrade; left out, the
image is `latest`. `docker compose up -d --build` builds the image from the
checkout instead.

Backing up, restoring and upgrading: [docs/backup-and-upgrade.md](docs/backup-and-upgrade.md).

E-readers: KOReader and OPDS in [docs/koreader.md](docs/koreader.md); the
companion app for Tolinos on Android firmware in
[clients/android/README.md](clients/android/README.md); Tolinos on the
Kobo-based firmware in [docs/tolino-kobo-mode.md](docs/tolino-kobo-mode.md).

## Development

Every change belongs to an issue and lives on a branch `<type>/<issue>-<slug>`;
see [CLAUDE.md](CLAUDE.md). `./run-tests.sh` is the gate that must pass before a push.

## Licence

[Apache 2.0](LICENSE)

The artwork is based on the Go gopher, designed by
[Renée French](https://go.dev/blog/gopher) and licensed under
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).
