# The API and API keys

Everything the web app does goes through GOtome's HTTP API, under
`/api/v1`. The OpenAPI document, `api/openapi.json` in the source, lists every route,
what it takes, what permission it needs and how to sign in to it (the
session cookie, or an API key as a bearer token). Scripts, home automation and other programs
can use it with an API key instead of a browser session.

## Making a key

On your profile, under **API keys**, give the key a name, tick its scopes
and, if you like, an expiry date. The key is shown **once**: GOtome keeps
only a hash of it, so a lost key cannot be looked up. Revoke it and make
another.

The list shows each key's name, its first characters, its scopes, when it
was made and last used, and its expiry. **Revoke** stops a key at once.
You can hold 50 keys.

## Scopes

A key does only what its scopes allow, and never more than your role
allows: if you are later made a Reader, an `upload` key of yours stops
uploading. A key made for a role that does not hold a scope is refused.

| Scope      | Lets the key | Role needed |
| ---------- | ------------ | ----------- |
| `read`     | view, read and download books, search, lists | Reader |
| `progress` | change your reading status, progress and shelves | Reader |
| `upload`   | upload books | Editor |
| `edit`     | edit metadata, start scans and re-indexing | Editor |
| `admin`    | manage accounts, settings and libraries on disk | Administrator |

A key cannot use the routes about your account itself: it cannot make or
list keys, change a password, end sessions or add devices. The one
exception is `GET /api/v1/auth/me`, which tells a script who the key signs
in as and which permissions it holds.

A disabled account's keys stop working, as do expired and revoked ones.
Wrong keys are slowed down like wrong passwords: after a few, the address
waits.

## Using a key

Send it as a bearer token:

```sh
KEY=gotome_...
curl -H "Authorization: Bearer $KEY" https://books.example.org/api/v1/auth/me

curl -H "Authorization: Bearer $KEY" \
  "https://books.example.org/api/v1/books?limit=20"

curl -H "Authorization: Bearer $KEY" -F file=@book.epub \
  https://books.example.org/api/v1/libraries/LIBRARY_ID/uploads
```

Over plain HTTP the key travels in the clear: use HTTPS (see
[Getting started](getting-started.md)).

Failures are always the same envelope: `{"error": {"code": "...",
"message": "..."}}`. `401` means the key is wrong, expired or revoked,
`403` that its scopes (or your role) do not reach that route, and `429`
that the address is waiting after wrong keys.

## Creating a key from a script

Keys can only be made from a signed-in browser session (the profile),
through `POST /api/v1/auth/api-keys` with `{"name": "...", "scopes":
["read"], "expiresAt": "2027-01-01T00:00:00Z"}`; the answer holds the
`token`.
