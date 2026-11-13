# E-readers and KOReader

GOtome is an OPDS catalogue: an e-reader app that reads OPDS, such as
KOReader, browses the library and downloads books from it, signed in with
a token of the device's own.

A Kobo reader, or a Tolino on the Kobo-based firmware in Kobo mode, needs
no app: it syncs with GOtome over its own sync, with the line the profile
shows for it ([tolino-kobo-mode.md](tolino-kobo-mode.md)). A Tolino on
Android firmware takes the companion app
([clients/android](../clients/android/README.md)).

## Adding a device

1. In GOtome, open your profile and, under **E-readers**, add a device by
   name (say, "Tolino").
2. GOtome shows the catalogue's address, your user name and the device's
   token. The token is shown this once; if it is lost, revoke the device
   and add it again.
3. In KOReader: the search menu, **OPDS catalog**, the **+** at the top,
   and enter
   - **Catalog name**: GOtome
   - **Catalog URL**: the address shown, such as
     `https://books.example.org/api/v1/opds`
   - **Username**: your user name
   - **Password**: the token, as shown or in capitals.

The device sees the books you see and nothing else of your account: its
token opens the catalogue and downloads, not the rest of GOtome. Revoking
the device on your profile signs it out for good. Wrong tokens are refused
for a while after five tries, as wrong passwords are.

Over the internet, put GOtome behind HTTPS: the token travels with every
request as the password of HTTP Basic auth.

## Sending books to a device

On a book's page, **Send to an e-reader** puts the book in the device's
queue; in the library, select books and send them all at once. The
catalogue's first entry on the device, **Sent to this device**, lists the
books waiting there: download one and it leaves the queue, and its page in
GOtome says since when the device has it. A book not yet downloaded can be
taken back from its page. Sending a book the device has already queues it
again.

## The GOtome plugin for KOReader

With the plugin, KOReader fetches what was sent by itself when Wi-Fi comes
up, without opening the catalogue.

1. On your GOtome profile, under **E-readers**, download the KOReader
   plugin: `gotome.koplugin.zip`, which knows the address you downloaded
   it from.
2. Unpack it into KOReader's `plugins` folder (on most devices
   `koreader/plugins/`, over USB), so that there is a folder
   `gotome.koplugin` in it, and restart KOReader.
3. In KOReader's menu, under **GOtome** (among the tools): enter your user
   name and the device's token, and choose the folder books go into
   (KOReader's home folder unless you choose another).

### At home and on the road

If GOtome is reached at two addresses, such as its address on your network
and the one behind your reverse proxy, name both in `GOTOME_PUBLIC_URLS`
(`http://192.168.1.5:8080,https://books.example.org`,
[configuration.md](configuration.md)). The plugin you download then knows
both, the one you downloaded it from first. When it fetches, it tries the
address that worked last, then the others in order, and waits four seconds
at most for each to answer; a server that answers with a refusal counts as
reached, so a wrong token is told and not tried at the next address. An
address typed under **Server** is tried first.

**Fetch books sent here** fetches at once; **Fetch when Wi-Fi comes up**
(on by default) does so every time the device connects. Each book is
saved under its title, a number added where a file of that name exists,
and leaves the queue once downloaded; one whose download broke off is
tried again the next time. KOReader's file browser shows the new books at
once.

### Highlights and notes

The plugin also sends the highlights and notes of the book being read, so
that they are in your library as well as on the device
([highlights.md](highlights.md)). **Send this book's highlights now**
(in the same menu, with a book open) does it at once; **Send highlights
when a book is closed** (on by default) does it whenever a book is closed
while Wi-Fi is on, and again when Wi-Fi comes up with a book open. It
reads KOReader's own record of them: the `annotations` list of the book's
sidecar (`metadata.epub.lua` in the book's `.sdr` folder) and, for the
older versions of KOReader, its `highlight` and `bookmarks` tables.

Every send is the book's whole set, which is how GOtome learns of a
highlight deleted on the device: the same highlight sent again changes
nothing, a changed note follows, and one no longer there is removed. A
book is named by the same checksum as the progress sync, so a book GOtome
does not know, or of a library you do not see, is left out. Highlights
made in GOtome, or by another device, are not touched by a send. A note
or colour you change in GOtome stays when the device sends again.

The token is kept in KOReader's settings on the device
(`settings/gotome.lua`), as KOReader keeps its other accounts.

## What the catalogue offers

- **Sent to this device**: the books sent here, until they are downloaded.
- **Recently added**: the newest books first.
- **Authors** and **Series**, alphabetically; a series's books in their
  order.
- **Shelves**: your collections and smart shelves, and those shared with
  you.
- **Reading** and **Want to read**: the books you have marked so.
- **Search** by title, author or series, from KOReader's search field.

Only books with a file an e-reader reads are listed (EPUB, Kindle formats,
PDF), each with a download per format, the best first, and its cover.

## Progress sync

KOReader's own **Progress sync** keeps where you are in a book in GOtome,
so the web reader and other devices pick up there, and KOReader is offered
a place you read to elsewhere.

In KOReader, with a book open: the tools menu, **Progress sync**,
**Custom sync server**, and enter the GOtome address followed by
`/api/v1/kosync`, such as `https://books.example.org/api/v1/kosync`. Then
**Login** with your user name and the device's token, typed exactly as
GOtome showed it (in small letters, with the dashes): KOReader sends a
checksum of what is typed. **Register** is refused: devices are added on
your profile. Leave **Document matching method** at **Binary**.

KOReader names a book by a checksum of samples of its file, which GOtome
works out for every ebook file within ten minutes of it arriving or
changing. A book of a library you do not see is a book GOtome does not
know.

What goes across:

- A place KOReader saves in an EPUB opens in the web reader at the same
  share of the book; KOReader gets its own place back exactly.
- A place the web reader saves in the same EPUB is offered in KOReader at
  the start of its chapter.
- In a PDF, the page, both ways.
- A place KOReader reports behind one another device saved since is not
  written over it; KOReader offers the further one the next time it asks.
