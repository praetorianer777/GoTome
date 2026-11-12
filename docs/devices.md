# E-readers

GOtome sends books to e-readers and keeps the reading place in step with
the web reader. Which way depends on the device:

| Device | How | Guide |
|---|---|---|
| KOReader, on any device it runs on | OPDS catalogue, the GOtome plugin, and KOReader's progress sync | [koreader.md](koreader.md) |
| Kobo | Kobo's own sync, pointed at GOtome | [tolino-kobo-mode.md](tolino-kobo-mode.md#4-point-the-reader-at-gotome) (the last step is the same on a Kobo) |
| Tolino Shine 5, Shine Color, Vision Color (Kobo-based firmware) | Kobo mode, then Kobo's sync | [tolino-kobo-mode.md](tolino-kobo-mode.md) |
| Tolino Shine 4 and older (Android firmware 16.x) | the GOtome for Tolino app | [clients/android/README.md](../clients/android/README.md) |
| Any other app that reads OPDS | the OPDS catalogue | [koreader.md](koreader.md#adding-a-device) (the same address, user name and token) |

## Adding a device

Every device signs in with a token of its own, never with your password.
On your profile, under **E-readers**, **Add a device** with a name, such as
"Kobo Libra". GOtome shows, once:

- the catalogue's address, for OPDS apps,
- your user name and the device's **token**, which an OPDS app takes as
  the password, and
- for a Kobo reader, the line for its config file.

Lost the token? **Revoke** the device and add it again. Revoking signs a
device out for good; disabling an account signs out all its devices.

The GOtome for Tolino app can instead ask to be added: it shows a
four-digit code, and your profile (and a notification) shows the request
with the same code to **Accept** or **Decline**.

When you add a device (or accept a request), **Allow this device to upload
books** lets the GOtome for Tolino app upload files of its own to GOtome
([the app's page](../clients/android/README.md#books-already-on-the-tolino)). Only
accounts that may upload books can use it, within their storage quota. The
files go into a managed library you own, else the first managed library you
see by name; with none, the app says so. The choice is made when the device
is added; to change it, revoke the device and add it again.

A device sees the books you see, and nothing else of your account: its
token opens the catalogue, downloads, covers and reading progress, not the
rest of GOtome. Wrong tokens are refused for a while after a few tries.

Over the internet, put GOtome behind HTTPS
([getting-started.md](getting-started.md#a-reverse-proxy-and-https)): a
device's token travels with each of its requests, and a Kobo's is part of
the address it calls.

## Sending books

On a book's page, **Send to an e-reader**; in the library, select books
and send them all at once. Each device then fetches them its own way: from
the **Sent to this device** entry of the OPDS catalogue or the KOReader
plugin, at a Kobo's next sync, or when the Tolino app next goes online.

A Kindle takes books by e-mail instead: see [Send to Kindle](kindle.md).

A device's page, reached from your profile, lists the books sent to it:
waiting, synced, or delivered. A book still waiting can be taken back; on
a Kobo, taking a book back removes it at the next sync.

## EPUBs that are slow on e-readers

Some EPUBs are hard on an e-reader: a chapter file of several megabytes
that it lays out at once, images of tens of megapixels, megabytes of
embedded fonts, chapter files that are not well-formed XHTML, a table of
contents that leads nowhere. GOtome measures every EPUB and shows such findings on the
book's page; the library's filter **On e-readers** finds them all, and the
Overview counts them. An editor can **Fix** a finding where the library is
writable ([troubleshooting.md](troubleshooting.md#e-reader-findings-and-fixes)).

### Sent fixed

A book sent to a device does not wait for anyone to press Fix, and does not
need a writable library. When it is queued, GOtome makes a copy of each of
its EPUBs that has findings in the background, with the fixes of
**Everything found** (fonts are cut down, not removed), and the device
downloads that copy. The library's file is only read, never changed. The
copies lie in `device-copies/` in the data folder; they are kept up to 4 GB
and 90 days unused, are made again when the file or GOtome's fixes change,
and are not in a backup. Turn it off with the setting
`devices.fixOnSend` ([configuration.md](configuration.md#devices)).

- A download never waits for a copy: if it is not ready yet (a large book
  takes a while), the device gets the file as it lies, and the next
  download gets the copy.
- A device's book list says **Sent fixed** for a book that was downloaded as
  a fixed copy.
- KOReader's progress sync names a book by the hash of the file it holds. A
  fixed copy has a hash of its own, which GOtome records, so the place is
  kept for the book either way. A place written by the web reader cannot be
  given to KOReader in a fixed copy (its chapters may be cut differently);
  KOReader's own places are kept.
- A copy that was removed to make room keeps its hashes, so a device that
  holds it is still known.
