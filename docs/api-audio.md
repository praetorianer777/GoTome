# The audio API for players

This is for people who write an audiobook player, or want to point an existing
one at GOtome. It lists the routes a player needs, in order of use, with the
answers the server really gives. The general rules of the API, error
envelope and OpenAPI document are in [The API and API keys](api.md).

All paths are under `/api/v1` on your GOtome address. The examples use
`https://books.example.org`, public-domain books and made-up IDs.

## The stable subset

These operations are marked in `api/openapi.json` with
`"x-gotome-stable": "audio/1"`:

| Operation | Route | Used for |
| --------- | ----- | -------- |
| `listBooks` | `GET /books` | finding audiobooks |
| `getSeries` | `GET /series/{seriesKey}` | a series and the next book |
| `getAudio` | `GET /books/{bookId}/audio` | the timeline: parts and chapters |
| `downloadFile` | `GET /files/{fileId}/download` | a part's bytes, with range requests |
| `getBookCover` | `GET /books/{bookId}/covers/{size}` | the cover |
| `getProgress` | `GET /books/{bookId}/progress` | where the person is |
| `putProgress` | `PUT /books/{bookId}/progress/{medium}` | saving where the person is |

Their paths, methods, parameters and the shape of their requests and answers
(descriptions aside) will not change while the version is `audio/1`. GOtome may
add a field to an answer, so ignore fields you do not know. A change that
could break a player gets a new version (`audio/2`) beside the old one, which
keeps working for as long as the release notes say. A test
(`TestTheStableSubsetKeepsItsShape`) fails when the shape changes without
that. Every other route may change at any release.

## Signing in

GOtome has two ways for a program to sign in, and both reach the whole subset:

- **An API key**, made on the profile under **API keys**, sent as
  `Authorization: Bearer gotome_...`. Give it the scopes `read` (listing,
  timeline, downloads, covers) and `progress` (saving the position). A key is
  one program's access and is revoked on its own. This is the right choice for
  a player that holds a key the person pastes in.
- **A device token**, made on the profile under devices (or by the pairing
  of an app, see [E-readers](devices.md)), sent as HTTP Basic auth with the
  user name and the token as the password. A device token reaches only the
  routes marked for devices: this subset, the OPDS feeds and a few more.
  Use it when the player is added as a device and listed with the person's
  devices.

```sh
curl -H "Authorization: Bearer $KEY" https://books.example.org/api/v1/auth/me
curl -u "reader:ABCDE-FGHJK-LMNPQ-RSTUV" https://books.example.org/api/v1/books?limit=1
```

There is no OAuth or other token flow. Over plain HTTP both travel in the
clear: use HTTPS. Wrong credentials answer `401`, and an address that sends
many wrong ones is made to wait (`429`). A book or file the person may not
see answers `404`, exactly as one that does not exist.

## Listing audiobooks

`GET /books` takes a rule tree as `filter`. Audio formats are `m4b`, `m4a`,
`mp3`, `flac`, `ogg` and `opus`:

```sh
curl -G -H "Authorization: Bearer $KEY" https://books.example.org/api/v1/books \
  --data-urlencode 'limit=2' --data-urlencode 'sort=title' \
  --data-urlencode 'filter={"all":[{"field":"format","op":"in","values":["m4b","mp3"]}]}'
```

```json
{
  "books": [
    {
      "id": "6f1c1a52-6d3a-4a43-9a0e-3f5d7d9a0b11",
      "libraryId": "0d9a4f1e-71c2-4b8e-8f0e-5c6b9d2a7e01",
      "title": "A Princess of Mars",
      "authors": ["Edgar Rice Burroughs"],
      "authorKeys": ["burroughs edgar rice"],
      "series": "Barsoom",
      "seriesKey": "barsoom",
      "seriesIndex": 1,
      "publishedYear": 1917,
      "coverKey": "9b0c...e41",
      "formats": ["m4b"],
      "addedAt": "2026-10-02T18:21:07Z",
      "status": "reading",
      "placeholder": false,
      "filesMissing": false,
      "ereaderFindings": []
    }
  ],
  "nextCursor": "eyJ..."
}
```

The page is cut by a cursor: send `nextCursor` back as `cursor` until it is
left out. `limit` is at most 200. `sort` is `title`, `author`, `added` or
`series`; `order` is `asc` or `desc`. `status` and `rating` are the person's
own. The field list in the OpenAPI document describes every other filter
field (author, narrator, language, genre, and so on).

A book may hold an ebook and an audiobook; its `formats` say which. Take
the audio ones.

## A series

`GET /series/{seriesKey}` (the `seriesKey` of a book) says how far the person
has read and which book is next:

```json
{
  "name": "Barsoom",
  "key": "barsoom",
  "books": 3,
  "read": 1,
  "next": { "id": "2a7e0c4b-...", "title": "The Gods of Mars", "seriesIndex": 2, "formats": ["mp3"], "...": "..." },
  "missing": [],
  "listed": [],
  "library": "0d9a4f1e-71c2-4b8e-8f0e-5c6b9d2a7e01"
}
```

To list the books of a series in their order, ask `GET /books` with
`sort=series` and a filter `{"all":[{"field":"series","op":"in","values":["barsoom"]}]}`.

## The timeline

`GET /books/{bookId}/audio` gives the book's audio as one recording. A
book may have many files; the timeline puts them one after another, and a
player keeps one clock for the whole book.

```json
{
  "parts": [
    {
      "fileId": "c1a7f0d2-2b61-4f34-a5e9-0d18e5c4b701",
      "format": "mp3",
      "mediaType": "audio/mpeg",
      "name": "01 - Chapter One.mp3",
      "startMs": 0,
      "durationMs": 1812000
    },
    {
      "fileId": "e4b9a3c6-8a0d-47fb-9d12-6f3c7a1e2802",
      "format": "mp3",
      "mediaType": "audio/mpeg",
      "name": "02 - Chapter Two.mp3",
      "startMs": 1812000,
      "durationMs": 2104000
    }
  ],
  "chapters": [
    { "title": "Chapter One", "fileId": "c1a7f0d2-...", "startMs": 0, "endMs": 1812000 },
    { "title": "Chapter Two", "fileId": "e4b9a3c6-...", "startMs": 1812000, "endMs": 3916000 }
  ],
  "durationMs": 3916000,
  "complete": true
}
```

- `parts` are in play order. `startMs` is where a part begins on the whole
  timeline; to play the time `t`, load the part with
  `startMs <= t < startMs + durationMs` and seek to `t - startMs`.
- `chapters` use the whole timeline's times as well, and `fileId` names the
  part they begin in. A book with one `m4b` has all its chapters in one part.
  A book without chapter marks gets one chapter per part, or none.
- `mediaType` is what the file is sent as; use it to ask your platform
  whether it can play the part. GOtome never transcodes.
- `complete` is false while a part has not been read yet; the timeline
  can still be played, but later parts and the total may change.

## Streaming a part

`GET /files/{fileId}/download` sends the file as it lies on disk. It answers
range requests, and `If-Range` with the `ETag`, which is the file's SHA-256:

```sh
curl -H "Authorization: Bearer $KEY" -H "Range: bytes=1048576-2097151" \
  -i https://books.example.org/api/v1/files/c1a7f0d2-2b61-4f34-a5e9-0d18e5c4b701/download
```

```
HTTP/1.1 206 Partial Content
Content-Range: bytes 1048576-2097151/29018112
Accept-Ranges: bytes
Content-Type: audio/mpeg
```

The connection is not cut by a write deadline, so a long file can stream.
Seek with ranges rather than reading from the start. Keep the credentials on
every request: a player that cannot send a header cannot play from GOtome.

## Covers

`GET /books/{bookId}/covers/small` or `.../large` is a JPEG. A book's
summary carries `coverKey`; send it as `?v=<coverKey>` and the answer may be
cached for a year (`Cache-Control: immutable`). Without it the answer is
revalidated with its `ETag`. A book without a cover answers `404`.

## Where the person is

`GET /books/{bookId}/progress` answers the saved positions, one per medium:

```json
{
  "audio": {
    "fileId": "e4b9a3c6-8a0d-47fb-9d12-6f3c7a1e2802",
    "locator": "2650000",
    "fraction": 0.6767,
    "chapter": "Chapter Two",
    "positionMs": 2650000,
    "clientId": "my-player-phone",
    "updatedAt": "2026-11-11T21:04:55Z"
  },
  "finishes": 0
}
```

`audio` is left out when nothing is saved. Open the book at `positionMs` on
the whole timeline, and remember `updatedAt`.

## Saving the position

`PUT /books/{bookId}/progress/audio`:

```json
{
  "fileId": "e4b9a3c6-8a0d-47fb-9d12-6f3c7a1e2802",
  "locator": "2650000",
  "positionMs": 2650000,
  "fraction": 0.6767,
  "chapter": "Chapter Two",
  "clientId": "my-player-phone",
  "basedOn": "2026-11-11T21:04:55Z"
}
```

- `locator` is the millisecond on the whole timeline as a string, and
  `positionMs` the same as a number. `fraction` is between 0 and 1 (position
  over `durationMs`). `clientId` names your player and stays the same on
  every write, at most 100 characters. `fileId`, `chapter` and `page` are
  optional.
- `basedOn` is the `updatedAt` you last read; leave it out when you read
  none. It lets GOtome tell that another client wrote since.
- Send a position when playback pauses, at a chapter change and every
  half minute or so while playing. A write from the same client within 30
  minutes extends one reading session; the first position makes the book
  `reading`, and reaching 99 percent counts a finish.

The answer when it was saved:

```json
{ "saved": true, "progress": { "locator": "2650000", "fraction": 0.6767, "clientId": "my-player-phone", "updatedAt": "2026-11-11T21:09:12Z", "...": "..." } }
```

When another client wrote a further position since you last read, yours is
not written and the status is still `200`:

```json
{ "saved": false, "progress": { "locator": "3100000", "fraction": 0.7916, "clientId": "my-player-tablet", "updatedAt": "2026-11-11T21:07:40Z", "...": "..." } }
```

`progress` is then the further position. Offer the person the choice; if they
keep their own, send the same body again with `"force": true`. If they take
the other, jump there and use its `updatedAt` as the next `basedOn`.
A position earlier than the saved one from the same client is written (a
person may go back).

Invalid values answer `422` with the error envelope and the field names.

## Known clients

None have been verified against this API yet. When a player is known to work,
it will be listed here with the version it was tried with. If you make one or
try one, say so in an issue.
