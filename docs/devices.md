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
