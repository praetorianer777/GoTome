# Getting started

From nothing to a library GOtome has scanned, in about a quarter of an
hour. You need:

- A machine running Linux with Docker and Docker Compose v2 (`docker
  compose version` answers). amd64 or arm64; a NAS works too
  ([platforms.md](platforms.md)).
- 2 GB of memory at the very least, 4 GB to work out similar books
  comfortably.
- Disk for the books you upload, the database and the search index: the
  search index of 48,000 EPUBs takes 7.6 GB.

## 1. Get the compose file

Everything a deployment needs is the folder `deploy` of the repository:

```bash
git clone https://github.com/praetorianer777/GoTome.git
cd GoTome/deploy
cp .env.example .env
```

In `.env`, uncomment and set what you want to differ from the defaults.
Two are worth deciding now:

- `GOTOME_IMAGE`: pin a release, such as
  `ghcr.io/praetorianer777/gotome:1.0.0`, so that an upgrade happens when
  you change it and not whenever a new image appears.
- `GOTOME_PORT`: the port on this machine, 8080 unless you change it.

Every variable is described in [configuration.md](configuration.md).

## 2. Mount your books

Books reach GOtome in two ways, and a library is one or the other:

- **Managed by GOtome**: GOtome keeps the folder in its data volume and
  arranges the files itself. Books arrive by upload (**Add books**).
- **An existing folder**: a folder of books you already have and arrange
  yourself. GOtome reads it and, unless you allow it, never changes
  anything in it.

For an existing folder, mount it into the app container. In
`docker-compose.yml`, under `app`, `volumes`, uncomment the example line
and give your own path:

```yaml
    volumes:
      - app-data:/data
      - secrets:/secrets:ro
      - /srv/books:/books:ro
```

`:ro` mounts it read-only: GOtome cannot change a file there, whatever
the library's settings say. Leave `:ro` off only if GOtome is to write
into that folder, which a library allows with **GOtome may change files
here**:

- it writes edited details and covers into EPUB files (only the metadata;
  the text stays as it is),
- **Move to trash** moves a file into the folder's `.trash`, and
- **Fix** for e-readers replaces an EPUB with a lighter one, the original
  in the trash.

The app runs as user 10001 in its container. The folder must be readable
by that user (and writable, for a writable library): either its files are
readable by everyone, which is usual, or give it to that user with
`chown -R 10001 /srv/books`.

Several folders are several mounts (`/books/fiction`, `/books/comics`)
and several libraries. Two libraries may not overlap.

How files are grouped into books: the files of one folder that share a
name are one book in several formats (`Dusk.epub`, `Dusk.pdf`), and the
numbered audio files of one folder, or of its `CD 1`, `CD 2` subfolders,
are one audiobook. A folder per book, named after it, works best.
GOtome reads EPUB, PDF, MOBI, AZW and AZW3, and audiobooks as M4B, M4A,
MP3, FLAC, Ogg and Opus.

The player keeps a queue for each person on the server, so it follows to
another browser. "Play next" and "Add to queue" are on an audiobook's page,
"Add to the queue" on a selection in a list, and "Queue the rest of this
series" on a series' page. The next book starts when one ends, from the
place it was left at; the queue is listed under the player, where books
are moved or taken out.

The sleep timer stops after 15 to 60 minutes or at the end of the chapter,
fading the volume down over its last 15 seconds. "Keep listening for 15
more minutes" extends it, also in the half minute after it stopped; on a
phone whose browser allows it (not iOS), shaking it does the same. The
player can also start a timer by itself when a book is played between two
hours you choose, for a length you choose. The volume boost raises the
volume up to twice through the browser's Web Audio, with a limiter against
clipping. The night hours and the boost are this browser's own.

## 3. Start it

```bash
docker compose up -d
docker compose ps
```

Three containers start:

1. **init** runs once and exits. On the first start it writes two files
   into the `secrets` volume: a random database password and the key that
   GOtome encrypts stored secrets (such as API tokens) with. On every
   later start it finds them there and changes nothing. Nobody has to
   choose or type either, and neither is in the compose file or an
   environment variable.
2. **db**, Postgres, reads the password from the volume and creates the
   database.
3. **app**, GOtome, waits for both, brings the database's tables up to
   date and starts. `docker compose ps` shows it `healthy` within a
   minute; after an upgrade whose migrations are long it shows `starting`
   until they are done (it answers `/readyz` with `migrating` meanwhile,
   see [troubleshooting.md](troubleshooting.md#monitoring)).

`docker compose logs -f app` shows what it does. If it stops, the last
lines say why ([troubleshooting.md](troubleshooting.md)).

Keep the `secrets` volume: without the key in it, the stored secrets
cannot be read again. A backup takes it along
([backup-and-upgrade.md](backup-and-upgrade.md)).

## 4. Create the first administrator

Open `http://<this machine>:8080`. The first page asks for a user name, a
password of at least 8 characters and, if you like, an e-mail address:
that account becomes the administrator. This page appears only once; afterwards, accounts are made
by an administrator on the **Users** page, or by signing in through an
identity provider ([single-sign-on.md](single-sign-on.md)).

Set the administrator up before the machine is reachable from the
internet: until then, whoever opens the page first becomes the
administrator.

## 5. Add a library

Administration, **Libraries**, **Add a library**:

- **Name**: what the library is called in GOtome.
- **Kind**: **Managed by GOtome**, or **An existing folder**.
- **Folder**: for an existing folder, the path *inside the container*,
  such as `/books`, as mounted in step 2. A managed library may leave it
  empty and gets one under `/data/libraries`.
- **Who sees it**: **Everybody** signed in, or **Only its members**
  ([users-and-libraries.md](users-and-libraries.md)).
- **GOtome may change files here**: only for an existing folder, and only
  with a mount without `:ro` (see step 2). A managed library always may.

**Add the library** scans it at once. The Libraries page shows the scan
and what it found; after that the folder is looked through again every
six hours (`GOTOME_SCAN_INTERVAL`), and **Scan now** does it at once.

## 6. Watch it fill

A scan only finds files and makes a book of each; the rest happens in the
background, file by file, and the **Overview** page shows how far it is:

1. **Files read**: each file's details, cover, chapters and text are read
   from it. Books appear in the library as soon as they are found, with
   their file name as title until their file is read.
2. **Texts in the search index**: the full-text search finds a book once
   its text is in.
3. **Books waiting to be looked up**: each book is looked up with the
   metadata sources, which fill in what it lacks ([metadata.md](metadata.md)).
   Matches GOtome is not sure of wait on the **Review** page.
4. **Duplicate checks**: books that are the same, or hold the same text,
   go to the **Duplicates** page, which shows each pair side by side,
   with what differs marked. **Keep this one** keeps a book and moves
   the other's files to the trash, with everyone's reading and shelves
   carried over; in a library GOtome does not write to, it merges the
   other book in, files and all. **Keep both** and **Merge…** are there
   too. Two audiobooks of one title and author read by different
   narrators are not copies: the page says "another narrator" and offers
   **Link as other recordings**, after which each book's page lists the
   other recordings with their narrators. The library's filter has a
   Narrator field.
5. **Books worked out for similar books**: the slowest part, a few
   seconds per book on a CPU. Pause it on the jobs page if the
   machine needs its CPU for other work.

The **Jobs** page lists every piece of work, with what failed and why. A
library of 50,000 books is read in some hours and worked out for similar
books in a day or two.

## 7. Invite people

Administration, **Users**, **Add a user**: a user name, a first password,
and a role ([users-and-libraries.md](users-and-libraries.md)). Each person
changes their password on their profile, chooses there what to be told of
([notifications.md](notifications.md)), and adds their e-readers
([devices.md](devices.md)).

## A reverse proxy and HTTPS

To reach GOtome from outside your network, put it behind a reverse proxy
that serves HTTPS. Over plain HTTP, passwords and device tokens travel
readable to anyone on the way.

### Nginx Proxy Manager

1. **Hosts**, **Proxy Hosts**, **Add Proxy Host**.
2. **Details**: the domain name, such as `books.example.org`; scheme
   `http`; forward hostname and port: the machine running GOtome and
   `GOTOME_PORT` (or, if Nginx Proxy Manager is on the same Docker network,
   `app` and `8080`). Turn on **Block Common Exploits**. **Websockets
   Support** is not needed.
3. **SSL**: request a Let's Encrypt certificate, and turn on **Force SSL**
   and **HTTP/2 Support**.
4. **Advanced**, so that large uploads (audiobooks) go through:

   ```nginx
   client_max_body_size 0;
   proxy_request_buffering off;
   ```

Nginx Proxy Manager passes on the host name, `X-Forwarded-Proto` and
`X-Forwarded-For` by itself. Tell GOtome to believe it (see
[Trust the proxy](#trust-the-proxy) below): in `.env`, with Nginx Proxy
Manager on the same Docker network, that network's range, which
`docker network inspect <network>` shows under `Subnet`:

```sh
GOTOME_TRUSTED_PROXIES=172.18.0.0/16
```

or, with Nginx Proxy Manager on another machine, that machine's address,
such as `GOTOME_TRUSTED_PROXIES=192.168.1.10`. When it runs on the same
machine but reaches GOtome through `GOTOME_PORT`, requests may arrive
from the gateway of GOtome's own network instead (`Gateway` in
`docker network inspect`, such as `172.19.0.1`).

### Any other proxy

GOtome needs three things of a proxy:

- **The host name the browser used**, in `Host`. GOtome refuses changes
  from a page whose origin is not the host it was asked as ("This request
  came from another site and was refused"), and builds the addresses it
  hands out (single sign-on, e-reader setup) from it.
- **`X-Forwarded-Proto: https`**, so that the session cookie is marked
  Secure and single sign-on sends people back over HTTPS.
- **`X-Forwarded-For`**, the address the request came from, so that the
  limits on failed sign-ins count each person apart.
- **Room for uploads**: a body as large as `GOTOME_UPLOAD_LIMIT_MB`, and
  no read timeout shorter than an upload takes.

In plain nginx:

```nginx
server {
    listen 443 ssl;
    http2 on;
    server_name books.example.org;
    # ssl_certificate and ssl_certificate_key as for your other sites

    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_request_buffering off;
    }
}
```

The notification count is a stream that stays open. GOtome tells nginx not
to buffer it and sends something every so often, so nothing is needed for
it.

### Under a path instead of a host name

To serve GOtome at `https://example.org/gotome/` beside other sites, set
`GOTOME_BASE_PATH=/gotome/` and keep the prefix in the proxy
([reverse-proxy-sub-path.md](reverse-proxy-sub-path.md)).

### Trust the proxy

Behind a proxy, every request comes from the proxy's address. Unless
GOtome is told which address that is, the limits per address (failed
sign-ins, e-readers asking to be added, single sign-on starts) count
everyone together, so one person guessing passwords locks out everybody
else for a few minutes. The limit per account holds either way.

Set `GOTOME_TRUSTED_PROXIES` in `.env` to the proxy's address, or a
range holding it, and `docker compose up -d`. From those addresses GOtome
takes the client's address from `X-Forwarded-For` (the last one in it
that is not a trusted proxy, as anything before it the client may have
written itself) and the scheme from `X-Forwarded-Proto`. From any other
address it ignores both, so name only the proxy: trusting a range that
holds other machines or containers lets them choose the address they
are counted under.

The addresses GOtome hands out (single sign-on, the Kobo and KOReader
setup) are built from the `Host` header and the scheme so worked out.
While `GOTOME_TRUSTED_PROXIES` is empty, `X-Forwarded-Proto` is believed
from anyone, as before the variable existed; once it is set, only from
the proxy.

With a proxy in front, keep GOtome's port off the internet: bind it to
the loopback address (`GOTOME_PORT=127.0.0.1:8080` in `.env`), or leave
it to your router not to forward it.

## Next

- [configuration.md](configuration.md): every variable and setting.
- [backup-and-upgrade.md](backup-and-upgrade.md): set up backups now.
- [devices.md](devices.md): KOReader, Kobo, Tolino.
- [troubleshooting.md](troubleshooting.md): when something does not work.
