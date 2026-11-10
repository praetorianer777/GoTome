# Configuration reference

GOtome is configured in two places:

- **Environment variables** (`GOTOME_*`), read once when the app starts.
  They say where things are and how the process runs. Changing one means
  restarting the app.
- **Settings**, which an administrator changes on the **Settings** page
  while GOtome runs. They are kept in the database, and take effect at
  once.

A test holds this page to the code: every variable GOtome reads and every
setting it has must be listed here.

## Environment variables

### How to set one

The compose file passes the variables in the table below on from `.env`,
the file beside it (copy `deploy/.env.example` to start one). For those,
a line in `.env` is all it takes:

```bash
# deploy/.env
GOTOME_SCAN_INTERVAL=1h
```

Then `docker compose up -d` restarts the app with it. A value GOtome cannot
use stops it at start with a message in the log
(`docker compose logs app`) that names the variable and what it wants.

### Set in `.env`

These are the ones meant to be changed. Every one has a default; the
compose file passes each on from `.env`.

| Variable | Default | What it does |
|---|---|---|
| `GOTOME_IMAGE` | `ghcr.io/praetorianer777/gotome:latest` | The app's image, for the compose file only. Pin a version, such as `ghcr.io/praetorianer777/gotome:1.0.0`, to choose when to upgrade ([backup-and-upgrade.md](backup-and-upgrade.md)). |
| `GOTOME_PORT` | `8080` | The port on the host the web app answers on, for the compose file only. Inside the container the app always listens on 8080. |
| `GOTOME_SCAN_INTERVAL` | `6h` | How often every library's folder is looked through for new, changed, moved and missing files: a duration of a minute or more, such as `30m` or `6h`. `0` turns scheduled scans off and leaves them to **Scan now** on the Libraries page. A new library is scanned at once either way. |
| `GOTOME_UPLOAD_LIMIT_MB` | `4096` | The largest file one upload may carry, in MiB, from 1 to 1048576. A reverse proxy in front has its own limit ([getting-started.md](getting-started.md#a-reverse-proxy-and-https)). Per-person quotas are set on the Users page. |
| `GOTOME_OFFLINE` | `false` | `true` keeps GOtome off the internet: no metadata source is asked (no lookups, no new books of followed authors), and no language model is downloaded. An embedding server you named on the Settings page is still used. |
| `GOTOME_EMBED_THREADS` | half the CPUs | How many CPU threads working out what a book is about may use, from 1 to 256. Lower it on a small machine to leave room for everything else ([troubleshooting.md](troubleshooting.md#similar-books-on-slow-hardware)). |
| `GOTOME_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `GOTOME_FORCE_PASSWORD_LOGIN` | `false` | `true` lets everyone sign in with a password even where the setting `auth.passwords` turned passwords off: the way back in when the identity provider is gone. Set it back once you are in. |
| `GOTOME_TRUSTED_PROXIES` | empty | The reverse proxies in front of GOtome, as addresses or CIDR ranges separated by commas, such as `172.18.0.0/16` or `192.168.1.10`. A request from one of them is taken to come from the address it names last in `X-Forwarded-For` that is not itself a trusted proxy, and over HTTPS when `X-Forwarded-Proto` says so; from anywhere else both headers are ignored. That keeps the limits on failed sign-ins, device requests and single sign-on starts per person rather than one for everyone behind the proxy ([getting-started.md](getting-started.md#a-reverse-proxy-and-https)). Empty trusts no forwarded address, and takes `X-Forwarded-Proto` from anyone, as before. |
| `GOTOME_ENV` | `production` | `production` logs one JSON object per line; `development`, for working on GOtome itself, logs plain text. |
| `POSTGRES_PASSWORD` | empty | Only for an installation that chose its own database password before `init` existed: `init` then writes that one into the secrets volume instead of a random one. Leave it unset otherwise. |

### Set by the compose file

The compose file sets these to what its containers need. Change them only
when you run GOtome some other way.

| Variable | In the compose file | What it does |
|---|---|---|
| `GOTOME_HTTP_ADDR` | `:8080` | The address and port the app listens on inside its container, as `host:port`. |
| `GOTOME_DATABASE_URL` | `postgres://gotome@db:5432/gotome?sslmode=disable` | The Postgres database, as a URL. Without a password in it when `GOTOME_DATABASE_PASSWORD_FILE` is set. |
| `GOTOME_DATABASE_PASSWORD_FILE` | `/secrets/db-password` | A file holding the database password, which goes into the URL above. `init` writes it, so the password is in no environment variable that `docker inspect` would show. |
| `GOTOME_SECRET_KEY_FILE` | `/secrets/secret-key` | A file holding the key that the secrets kept in the database (provider tokens, the identity provider's client secret) are encrypted with. `init` writes it once. GOtome does not start without it, nor with another key than the one the database's secrets were encrypted with. Back it up ([backup-and-upgrade.md](backup-and-upgrade.md)). |
| `GOTOME_SECRETS_DIR` | not set (`/secrets`) | Where `gotome init` writes the database password and the secret key. Only `init` reads it. |

### Usually left alone

The image's defaults are right for these. They are there for an unusual
machine or layout, or for running GOtome without the compose file, which
does not pass them on from `.env`: each names a place inside the
container, and a path there means nothing without a volume or a file
mounted at it, which `.env` cannot do.

To keep the language models (120 MB to 2.3 GB each) on another disk,
mount it at `/data/models` rather than move `GOTOME_MODEL_DIR`. Rather
than edit `docker-compose.yml`, which an upgrade replaces, put the mount
in a `docker-compose.override.yml` beside it, which Docker Compose reads
as well:

```yaml
# deploy/docker-compose.override.yml
services:
  app:
    volumes:
      - /mnt/big-disk/gotome-models:/data/models
```

The folder must be writable by the app's user, 10001.

| Variable | Default | What it does |
|---|---|---|
| `GOTOME_DATA_DIR` | `/data` | Where GOtome keeps its own files: managed libraries (`libraries/`), covers (`covers/`), the full-text index (`search/`) and language models (`models/`). The compose file mounts the `app-data` volume there. A full path. |
| `GOTOME_MODEL_DIR` | `$GOTOME_DATA_DIR/models` | Where language models for similar books are downloaded to and read from. A full path. |
| `GOTOME_ONNXRUNTIME` | `/usr/local/lib/libonnxruntime.so.1.30.0` | The ONNX Runtime library the language model runs on. The image carries it there. |
| `GOTOME_FULLTEXT_LIBRARY` | `/usr/local/lib/libgotome_fulltext.so` | GOtome's full-text search engine. The image carries it there. |

`GOMEMLIMIT`, Go's own variable, is not needed: GOtome sets its memory
limit to three quarters of what its container may use. Set it only to
choose another limit.

### For developing GOtome

These are read only by GOtome's own tests and tools, or by the app on
the development stack, never by an installation.

| Variable | What it does |
|---|---|
| `GOTOME_TEST_DATABASE_URL` | The Postgres server the integration tests make their databases on. |
| `GOTOME_RECORD_FIXTURES` | Set, the metadata sources' tests record the answers they lack from the live services. |
| `GOTOME_TEST_METADATA` | A JSON file of book records that the development stack (`deploy/docker-compose.dev.yml`) has the app look books up in, in place of every metadata source, for the browser tests: `e2e/fixtures/metadata.json`. Never set it in an installation. |
| `GOTOME_TEST_EMBEDDINGS` | `true` has the app embed books with vectors made from the words of their texts instead of a model (`embed.Words`), so the browser tests see similar books, searches by description and genre suggestions without one: books that share words are near. The development stack sets it, an installation never. |

## Settings

Administrators change these on the **Settings** page (Administration,
Settings). The key is what the API calls a setting
(`GET`/`PATCH /api/v1/settings`); the page shows the name beside it, in
sections as the headings below have them. A value the page refuses is
explained under its field. Emptying a field takes the setting back to its
default.

A setting of a few values is chosen, not typed: `on` or `off` is a
switch, one of several a list, and `metadata.providers` a box per
source. The API names those values as the setting's `choices`, with
`multiple` where the value is several of them separated by commas, and
refuses any other.

A secret is stored encrypted with the secret key. Once saved it is never
shown again, not even to administrators: the page says only when it was
saved. Type a new value to replace it.

### Book details from metadata sources

See [metadata.md](metadata.md).

| Key | On the page | Default | What it does |
|---|---|---|---|
| `metadata.language` | Language for book details | `en` | The language descriptions and other details are asked for in, as a two- or three-letter code such as `en` or `de`. A book's own language comes first where it has one. New books of followed authors and series are listed in this language and in English. |
| `metadata.googleBooksKey` | Google Books API key | none (secret) | Kept for a Google Books source. This version has no such source, so the key is not used. |
| `metadata.hardcoverToken` | Hardcover API token | none (secret) | The API token of your Hardcover account (hardcover.app, account settings, API). Without one Hardcover is not asked. |
| `metadata.autoMatch` | Look up new books by themselves | `on` | `on` looks every book up once its file is read; a match sure enough fills in what the book lacks, any other waits on the **Review** page. `off` leaves lookups to the book page and bulk changes. |
| `metadata.matchThreshold` | Sure enough to take without asking | `0.95` | The score, from 0.5 to 1, from which a match found by itself is taken without review. A shared ISBN or DOI scores 1; title, author and year alone at most 0.99. |
| `metadata.providers` | Sources to ask | `openlibrary,crossref,hardcover` | The sources asked: `openlibrary`, `crossref` and `hardcover`, separated by commas (a box each on the page). Leave one out to stop asking it; at least one stays. |
| `metadata.contactEmail` | Contact address for sources | none | An e-mail address given to sources that ask whom to write to about the requests they get. CrossRef serves requests with one faster. It is not stored with the answers. |

### Updates

| Key | On the page | Default | What it does |
|---|---|---|---|
| `updates.check` | Check for new releases | `on` | Once a day GOtome asks GitHub for the latest release and tells administrators of a newer one (footer, overview, a notification). GitHub learns that an instance exists at this address and nothing else; nothing is downloaded or installed. `GOTOME_OFFLINE=true` asks nothing whatever this says. |

### Trash

| Key | On the page | Default | What it does |
|---|---|---|---|
| `trash.retentionDays` | Days a file stays in the trash | `30` | How many days a file moved to the trash stays restorable before it is deleted for good, from 1 to 3650. |

### Similar books and searching by description

GOtome works out what each book is about with a language model, for
**Similar books** on a book's page and the search by description. See
[troubleshooting.md](troubleshooting.md#similar-books-on-slow-hardware) and
[embedding-server.md](embedding-server.md).

| Key | On the page | Default | What it does |
|---|---|---|---|
| `embedding.enabled` | Work out what books are about | `on` | `on` reads every book in the background, one at a time. `off` pauses that at the next book; on again goes on where it stopped. The jobs page has the same switch as **Pause** and **Start**. |
| `embedding.model` | Language model | `multilingual-e5-small` | The model: `multilingual-e5-small` (int8 weights, 120 MB, about 700 MB of memory), `multilingual-e5-small-fp32` (the same model in full precision, 470 MB, about 1.1 GB, faster on CPUs without int8 instructions), or `bge-m3` (larger and better, 2.3 GB and about 3 GB of memory on the CPU, or on an embedding server). Another model reads every book again; the files and vectors of the one before are kept for `embedding.removeUnusedAfterDays`, so going back within that time costs nothing. |
| `embedding.passages` | A vector for every passage | `off` | `on`, while books are embedded, gives every passage of a book's text a vector of its own, so that the search by description finds a book by the passage nearest it, a scene rather than only what the book is about as a whole. It reads every book again: on a server without a GPU the first pass takes days for a large library. The vectors take about 1.5 GB per 50,000 books under `/data/passages`, which a backup leaves out and GOtome makes again. Why and how: [decisions/passage-vectors.md](decisions/passage-vectors.md). |
| `embedding.removeUnusedAfterDays` | Days an unused model is kept | `1` | Once a model has not been the one in use for this many days (1 to 3650), its downloaded files under `GOTOME_MODEL_DIR`, its vectors in the database and its passage vectors are removed, within the hour. Never the model in use; a model on an embedding server has no files here, only its vectors go. The model list on the settings page shows what each model takes on disk and in the database, and **Remove now** removes one that is not in use at once (after the embedding pass running then pauses). Going back to a removed model downloads it again and embeds every book again. |
| `embedding.server` | Embedding server | none | The address of an Ollama server that runs `bge-m3`, such as `http://host.docker.internal:11434`, without a path. GOtome may reach this one address even on your own network. Empty, `bge-m3` runs in GOtome on the CPU. |

### Genres

See [genres.md](genres.md).

| Key | On the page | Default | What it does |
|---|---|---|---|
| `genres.suggest` | Genres for books | `off` | How books are given genres of GOtome's fixed list: `off` writes none; `tags` gives each book the genres its tags mean; `vector`, `neighbours` and `combined` also suggest genres from what a book is about, from the books most like it, or the second and, where it cannot tell, the first, for review under **Genres**. Use `neighbours` ([genres.md](genres.md)). The last three need similar books worked out; measure them with `gotome genre-check` first. A book's tags are never changed. |
| `curation.duplicates.classes` | Duplicates resolved by themselves | empty | Which classes of open duplicate pairs the rules handle every day without being asked, comma-separated, from `same_text`, `overlap_high`, `overlap_mid`, `contained` and `part_inside`. Empty handles none. The first three merge, the other two keep both books and link them; a run can be undone under **Changes** ([duplicates.md](duplicates.md)). Measure with `gotome duplicate-check` first. |
| `curation.duplicates.onConflict` | A pair whose books differ | `skip` | What a run does with a pair whose books differ in a field no rule can decide: `skip` leaves it whole for a person; `mergeAgreeing` merges it, takes what agrees and keeps the surviving book's value of the rest. |

### Signing in

See [single-sign-on.md](single-sign-on.md).

| Key | On the page | Default | What it does |
|---|---|---|---|
| `auth.passwords` | Sign in with a password | `on` | `off`, while an identity provider is set up, leaves single sign-on as the only way in, except for administrators while none of them has linked an account at the identity provider. Device tokens are not affected. `GOTOME_FORCE_PASSWORD_LOGIN=true` overrides it. |
| `oidc.issuer` | Identity provider: issuer URL | none | The issuer URL of an OpenID Connect provider, such as `https://auth.example.org/application/o/gotome/`. Empty, there is no single sign-on. Plain `http://` is accepted for a provider on your own network. |
| `oidc.clientId` | Identity provider: client ID | none | The client ID the provider gave GOtome. |
| `oidc.clientSecret` | Identity provider: client secret | none (secret) | The client's secret, unless the provider made GOtome a public client. |
| `oidc.name` | Identity provider: name on the button | `single sign-on` | The sign-in page offers "Sign in with" this. |
| `oidc.scopes` | Identity provider: scopes | `openid profile email` | The scopes asked for, separated by spaces, with `openid` among them. Add the one that brings the groups (often `groups`) if the provider wants it asked for. |
| `oidc.groupsClaim` | Identity provider: groups claim | `groups` | The claim of the ID token that lists a person's groups. |
| `oidc.adminGroups` | Groups of administrators | none | Groups, separated by commas, whose members are administrators. Once any of the three group settings is set, the groups decide everyone's role at every sign-in; the last administrator is never demoted so. |
| `oidc.editorGroups` | Groups of editors | none | Groups whose members are editors. |
| `oidc.readerGroups` | Groups of readers | none | Groups whose members are readers. |
| `oidc.defaultRole` | Role of everyone else | `reader` | The role of someone in none of the groups: `admin`, `editor`, `reader`, or `none` to let only the groups in. While no groups are set, the role of an account made at a first sign-in. |
| `oidc.signup` | Make accounts at first sign-in | `on` | `on` makes an account for someone who signs in through the provider for the first time. `off` lets in only accounts already linked, or found by their address. |
| `oidc.linkByEmail` | Find accounts by e-mail address | `off` | `on` lets a first sign-in take over the account with the e-mail address the provider says it has verified. Only for a provider that verifies addresses. |

### Mail (Send to Kindle)

Books go to a Kindle as an e-mail attachment: the EPUB, unchanged, up to 50 MB (Amazon's limit; your mail server may allow less). Each person sets their Kindle address on their profile (`name@kindle.com` only) and approves the sender address in their Amazon account. See [Send to Kindle](kindle.md).

| Key | On the page | Default | What it does |
|---|---|---|---|
| `mail.host` | Mail server | none | The SMTP server books are sent to a Kindle through ("Send to Kindle"); none turns the feature off, as does `GOTOME_OFFLINE=true`. A name without a port. It is the only host GOtome sends mail to, and the one administrator-named host GOtome dials whatever address it resolves to, a local mail server included. |
| `mail.port` | Mail server port | `587` | The server's port: 587 for STARTTLS, 465 for TLS. |
| `mail.security` | Mail encryption | `starttls` | `starttls` upgrades the connection and refuses to send if the server does not offer it, `tls` is TLS from the first byte, `none` sends in the clear (and offers no password to any host but localhost). |
| `mail.user` | Mail user name | none | Signs in with this and `mail.password` (plain authentication, only ever over TLS); empty signs in as nobody. |
| `mail.password` | Mail password | none (secret) | Kept sealed, never sent back, never logged. |
| `mail.sender` | Sender address | none | The From address, which each person has to approve in their Amazon account or Amazon drops the mail. Without it the feature is off. |
